package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type BuildStatus struct {
	ID         int64     `json:"id,omitempty"`
	Queued     int       `json:"queued"`
	State      string    `json:"state"`
	StartedAt  time.Time `json:"startedAt,omitzero"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
	Log        string    `json:"log"`
	Error      string    `json:"error,omitempty"`
}

type BuildManager struct {
	root               string
	basePath           string
	apiBasePath        string
	timeout            time.Duration
	mu                 sync.Mutex
	status             BuildStatus
	queue              []int64
	history            *History
	onFinish           func(BuildStatus)
	preparePublication func(context.Context, map[string]string) error
	publicationInputs  func(context.Context) ([]Note, []string, error)
}

// RenderWorkerArg selects the subprocess owned by a running build. Keeping
// native DLLs in that process isolates their lifetime and the build timeout.
const RenderWorkerArg = "--render-worker"
const RenderManifest = "publication.json"

type RenderedPublication struct {
	Files    map[string]string `json:"files"`
	Versions map[string]string `json:"versions"`
}

type RenderJob struct {
	Root     string
	BasePath string
	Notes    []Note
}

func NewBuildManager(root string, timeout time.Duration, basePath, apiBasePath string, history *History,
	publicationInputs func(context.Context) ([]Note, []string, error), preparePublication func(context.Context, map[string]string) error) *BuildManager {
	if basePath == "" {
		basePath = "/"
	}
	return &BuildManager{
		root: root, basePath: basePath, apiBasePath: apiBasePath, timeout: timeout, history: history,
		status: BuildStatus{State: "idle"}, preparePublication: preparePublication, publicationInputs: publicationInputs,
	}
}

func (b *BuildManager) Status() BuildStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.statusLocked()
}

func (b *BuildManager) statusLocked() BuildStatus {
	status := b.status
	status.Queued = len(b.queue)
	return status
}

func (b *BuildManager) SetOnFinish(callback func(BuildStatus)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onFinish = callback
}

func (b *BuildManager) Resume(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status.State == "running" {
		return nil
	}
	ids, err := b.history.resumeBuildQueue(ctx)
	if err != nil {
		return err
	}
	b.queue = ids
	b.startNext()
	return nil
}

func (b *BuildManager) Start(ctx context.Context) (BuildStatus, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id, err := b.history.enqueueBuild(ctx)
	if err != nil {
		return b.statusLocked(), err
	}
	b.queue = append(b.queue, id)
	if b.status.State != "running" {
		b.startNext()
	}
	return b.statusLocked(), nil
}

// startNext is called with the mutex held, after the previous task has finished.
func (b *BuildManager) startNext() {
	if len(b.queue) == 0 {
		return
	}
	id := b.queue[0]
	b.queue = b.queue[1:]
	b.status = BuildStatus{ID: id, State: "running", StartedAt: time.Now()}
	go b.run(b.status, b.onFinish)
}

func (b *BuildManager) appendLog(text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status.Log += text
	if len(b.status.Log) > 128*1024 {
		b.status.Log = b.status.Log[len(b.status.Log)-128*1024:]
	}
}

type buildWriter struct{ manager *BuildManager }

func (w buildWriter) Write(data []byte) (int, error) {
	w.manager.appendLog(string(data))
	return len(data), nil
}

func (b *BuildManager) build(ctx context.Context) error {
	settingsPath := filepath.Join(b.root, "web", "public", "site-settings.json")
	if err := b.history.ExportPublicSettings(ctx, settingsPath); err != nil {
		return fmt.Errorf("导出站点设置: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	run := func(command *exec.Cmd) error {
		b.appendLog(fmt.Sprintf("\n$ %v\n", command.Args))
		writer := buildWriter{manager: b}
		command.Stdout = writer
		command.Stderr = writer
		command.WaitDelay = 5 * time.Second
		return command.Run()
	}
	notes, assets, err := b.publicationInputs(ctx)
	if err != nil {
		return err
	}
	job, _ := json.Marshal(RenderJob{Root: b.root, BasePath: b.basePath, Notes: notes})
	render := exec.CommandContext(ctx, executable, RenderWorkerArg)
	render.Dir = b.root
	render.Stdin = bytes.NewReader(job)
	if err := run(render); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(b.root, "web", "src", "generated", RenderManifest))
	if err != nil {
		return err
	}
	var manifest RenderedPublication
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	if err := b.preparePublication(ctx, manifest.Versions); err != nil {
		b.appendLog(fmt.Sprintf("\n[annotations] %v\n", err))
		return err
	}
	return publishFrontend(ctx, b.root, b.basePath, b.apiBasePath, manifest, assets, buildWriter{manager: b})
}

func (b *BuildManager) run(started BuildStatus, onFinish func(BuildStatus)) {
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	runErr := b.history.updateQueuedBuild(ctx, started)
	if runErr == nil {
		runErr = b.build(ctx)
	}
	if runErr != nil && ctx.Err() != nil {
		runErr = fmt.Errorf("构建超时: %w", ctx.Err())
	}
	b.mu.Lock()
	status := b.status
	b.mu.Unlock()
	status.FinishedAt = time.Now()
	if runErr != nil {
		status.State = "failed"
		status.Error = runErr.Error()
	} else {
		status.State = "success"
	}
	recordCtx, cancelRecord := context.WithTimeout(context.Background(), 5*time.Second)
	if err := b.history.updateQueuedBuild(recordCtx, status); err != nil {
		log.Printf("record build status: %v", err)
	}
	cancelRecord()
	if onFinish != nil {
		onFinish(status)
	}
	b.mu.Lock()
	b.status = status
	b.startNext()
	b.mu.Unlock()
}
