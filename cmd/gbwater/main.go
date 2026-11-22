package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"gbwater/internal/app"
	"gbwater/internal/render"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == app.RenderWorkerArg {
		var job app.RenderJob
		if err := json.NewDecoder(os.Stdin).Decode(&job); err != nil {
			log.Fatalf("读取构建任务: %v", err)
		}
		if err := render.Run(context.Background(), job.Root, job.BasePath, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) != 1 {
		log.Fatal("启动服务无需参数，请从后台「构建并发布」")
	}
	runServer()
}

func runServer() {
	config, err := app.LoadConfig(os.Getenv("GBWATER_ROOT"))
	if err != nil {
		log.Fatal(err)
	}
	application, err := app.NewServer(config)
	if err != nil {
		log.Fatal(err)
	}
	defer application.Close()
	if err := application.Bootstrap(context.Background()); err != nil {
		log.Printf("引导管理员账号失败: %v", err)
	}
	go application.SyncContent()
	server := &http.Server{
		Addr:              config.Addr,
		Handler:           application,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	log.Printf("GBwater API listening on http://%s", config.Addr)
	if config.Mail.Mode == "log" {
		log.Print("未配置 SMTP，验证码邮件将写入服务日志（GBWATER_MAIL_MODE=log）")
	}
	log.Fatal(server.ListenAndServe())
}
