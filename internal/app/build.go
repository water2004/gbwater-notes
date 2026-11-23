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
	"slices"
	"sync"
	"time"
)

type BuildMode string

const (
	BuildIncremental BuildMode = "incremental"
	BuildNote        BuildMode = "note"
	BuildAll         BuildMode = "all"
)

type BuildRequest struct {
	Mode   BuildMode `json:"mode,omitempty"`
	NoteID string    `json:"noteId,omitempty"`
}

type BuildTask struct {
	ID int64 `json:"id,omitempty"`
	BuildRequest
}

type BuildStatus struct {
	BuildTask
	Queued     int         `json:"queued"`
	Queue      []BuildTask `json:"queue,omitempty"`
	State      string      `json:"state"`
	StartedAt  time.Time   `json:"startedAt,omitzero"`
	FinishedAt time.Time   `json:"finishedAt,omitzero"`
	Log        string      `json:"log"`
	Error      string      `json:"error,omitempty"`
}

type BuildManager struct {
	root               string
	basePath           string
	apiBasePath        string
	timeout            time.Duration
	mu                 sync.Mutex
	status             BuildStatus
	queue              []BuildTask
	history            *History
	onFinish           func(BuildStatus)
	preparePublication func(context.Context, string, map[string]string) error
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
	Force    bool
	Output   string
}

func NewBuildManager(root string, timeout time.Duration, basePath, apiBasePath string, history *History,
	publicationInputs func(context.Context) ([]Note, []string, error), preparePublication func(context.Context, string, map[string]string) error) *BuildManager {
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
	status.Queue = slices.Clone(b.queue)
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

func (b *BuildManager) Start(ctx context.Context, request BuildRequest) (BuildStatus, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, task := range b.queue {
		if task.BuildRequest == request {
			return BuildStatus{BuildTask: task, State: "queued"}, nil
		}
	}
	id, err := b.history.enqueueBuild(ctx, request)
	if err != nil {
		return b.statusLocked(), err
	}
	task := BuildTask{ID: id, BuildRequest: request}
	b.queue = append(b.queue, task)
	if b.status.State != "running" {
		b.startNext()
		return b.statusLocked(), nil
	}
	return BuildStatus{BuildTask: task, State: "queued"}, nil
}

// startNext is called with the mutex held, after the previous task has finished.
func (b *BuildManager) startNext() {
	if len(b.queue) == 0 {
		return
	}
	task := b.queue[0]
	b.queue = b.queue[1:]
	b.status = BuildStatus{BuildTask: task, State: "running", StartedAt: time.Now()}
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

func (b *BuildManager) build(ctx context.Context, request BuildRequest) error {
	public := filepath.Join(b.root, "web", "public")
	generated := filepath.Join(b.root, "web", "src", "generated")
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
	job := RenderJob{Root: b.root, BasePath: b.basePath, Notes: notes, Force: request.Mode != BuildIncremental}
	if request.Mode == BuildNote {
		index := slices.IndexFunc(notes, func(note Note) bool { return note.ID == request.NoteID })
		if index < 0 {
			return fmt.Errorf("笔记已移除: %s", request.NoteID)
		}
		job.Notes = notes[index : index+1]
		if err := os.MkdirAll(generated, 0o755); err != nil {
			return err
		}
		job.Output, err = os.MkdirTemp(generated, ".note-build-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(job.Output)
	} else if err := b.history.ExportPublicSettings(ctx, filepath.Join(public, "site-settings.json")); err != nil {
		return fmt.Errorf("导出站点设置: %w", err)
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	render := exec.CommandContext(ctx, executable, RenderWorkerArg)
	render.Dir = b.root
	render.Stdin = bytes.NewReader(payload)
	if err := run(render); err != nil {
		return err
	}
	state, output := generated, public
	if job.Output != "" {
		state, output = job.Output, job.Output
	}
	var manifest RenderedPublication
	if err := readSiteJSON(filepath.Join(state, RenderManifest), &manifest); err != nil {
		return err
	}
	if len(manifest.Versions) > 0 {
		if err := b.preparePublication(ctx, output, manifest.Versions); err != nil {
			b.appendLog(fmt.Sprintf("\n[annotations] %v\n", err))
			return err
		}
	}
	if request.Mode == BuildNote {
		return publishSingleNote(ctx, b.root, b.basePath, b.apiBasePath, output, job.Notes[0], manifest, assets, buildWriter{manager: b})
	}
	return publishFrontend(ctx, b.root, b.basePath, b.apiBasePath, manifest, assets, buildWriter{manager: b})
}

func (b *BuildManager) run(started BuildStatus, onFinish func(BuildStatus)) {
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	runErr := b.history.updateQueuedBuild(ctx, started)
	if runErr == nil {
		runErr = b.build(ctx, started.BuildRequest)
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
