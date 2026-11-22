package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type BuildStatus struct {
	State      string    `json:"state"`
	StartedAt  time.Time `json:"startedAt,omitzero"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
	Log        string    `json:"log"`
	Error      string    `json:"error,omitempty"`
}

type BuildManager struct {
	root     string
	dataDir  string
	basePath string
	timeout  time.Duration
	mu       sync.Mutex
	status   BuildStatus
	onFinish func(BuildStatus)
}

var errBuildRunning = errors.New("已有一次构建正在运行")

// RenderWorkerArg selects the subprocess owned by a running build. Keeping
// native DLLs in that process isolates their lifetime and the build timeout.
const RenderWorkerArg = "--render-worker"

type RenderJob struct {
	Root     string
	BasePath string
}

func NewBuildManager(root, dataDir string, timeout time.Duration, basePath string) *BuildManager {
	if basePath == "" {
		basePath = "/"
	}
	return &BuildManager{root: root, dataDir: dataDir, basePath: basePath, timeout: timeout, status: BuildStatus{State: "idle"}}
}

func (b *BuildManager) Status() BuildStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

func (b *BuildManager) SetOnFinish(callback func(BuildStatus)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onFinish = callback
}

func (b *BuildManager) Start(prepare func() error) (BuildStatus, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status.State == "running" {
		return b.status, errBuildRunning
	}
	// 在取得构建名额后导出设置，避免其它请求改写本次构建正在使用的输入。
	if err := prepare(); err != nil {
		return b.status, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	b.status = BuildStatus{State: "running", StartedAt: time.Now()}
	go b.run(ctx, cancel, b.onFinish)
	return b.status, nil
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

func (b *BuildManager) run(ctx context.Context, cancel context.CancelFunc, onFinish func(BuildStatus)) {
	defer cancel()
	var runErr error
	executable, err := os.Executable()
	if err != nil {
		runErr = err
	} else {
		job, _ := json.Marshal(RenderJob{Root: b.root, BasePath: b.basePath})
		render := exec.CommandContext(ctx, executable, RenderWorkerArg)
		render.Dir = b.root
		render.Stdin = bytes.NewReader(job)
		frontend := exec.CommandContext(ctx, "node", filepath.Join("scripts", "build.mjs"))
		frontend.Dir = filepath.Join(b.root, "web")
		frontend.Env = append(os.Environ(), "GBWATER_BASE_PATH="+b.basePath)
		for _, command := range []*exec.Cmd{render, frontend} {
			b.appendLog(fmt.Sprintf("\n$ %v\n", command.Args))
			writer := buildWriter{manager: b}
			command.Stdout = writer
			command.Stderr = writer
			command.WaitDelay = 5 * time.Second
			if err := command.Run(); err != nil {
				runErr = err
				if ctx.Err() != nil {
					runErr = fmt.Errorf("构建超时: %w", ctx.Err())
				}
				break
			}
		}
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
	if data, err := json.MarshalIndent(status, "", "  "); err == nil {
		if err := atomicWrite(filepath.Join(b.dataDir, "build-status.json"), string(data)); err != nil {
			log.Printf("save build status: %v", err)
		}
	}
	if onFinish != nil {
		onFinish(status)
	}
	b.mu.Lock()
	b.status = status
	b.mu.Unlock()
}
