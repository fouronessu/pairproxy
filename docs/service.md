# Go Windows Service 开发指南

使用 `kardianos/service` 库开发原生 Windows 服务程序。

## 特点

- **跨平台**：代码同时支持 Windows/Linux/macOS
- **自包含**：无需外部工具（如 NSSM）包装
- **命令行管理**：内置 install/start/stop/uninstall 等命令
- **生产就绪**：支持优雅退出、日志记录、延迟启动

## 快速开始

### 1. 安装依赖

```bash
go get github.com/kardianos/service
```

### 2. 完整代码模板

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kardianos/service"
)

// ==================== 配置区域 ====================

const (
	serviceName        = "MyGoService"           // 服务系统名称（唯一标识）
	serviceDisplayName = "My Go Service"         // 显示名称
	serviceDescription = "A background service"  // 服务描述
)

// ==================== Logger 实现 ====================

type logger struct {
	log *log.Logger
}

func (l *logger) Error(v ...interface{}) error {
	l.log.Println("ERROR:", fmt.Sprint(v...))
	return nil
}

func (l *logger) Warning(v ...interface{}) error {
	l.log.Println("WARNING:", fmt.Sprint(v...))
	return nil
}

func (l *logger) Info(v ...interface{}) error {
	l.log.Println("INFO:", fmt.Sprint(v...))
	return nil
}

// ==================== 服务程序主体 ====================

type program struct {
	exit    chan struct{}      // 用于优雅退出
	service service.Service    // 服务实例
	logger  service.Logger     // 日志接口
}

// Start 服务启动时调用（由 service 框架调用）
func (p *program) Start(s service.Service) error {
	p.service = s
	p.exit = make(chan struct{})

	// 必须在 goroutine 中运行业务逻辑，不能阻塞 Start 方法
	go p.run()
	return nil
}

// run 实际业务逻辑运行在这里
func (p *program) run() {
	p.logger.Info("服务启动成功")

	// 创建定时任务示例（每30秒执行一次）
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// 捕获系统信号（Ctrl+C、系统关机等）
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-ticker.C:
			// 执行你的业务任务
			p.doWork()

		case <-sigChan:
			p.logger.Info("收到系统信号，准备退出")
			return

		case <-p.exit:
			p.logger.Info("收到停止请求")
			return
		}
	}
}

// doWork 在这里编写你的业务代码
func (p *program) doWork() {
	p.logger.Info("执行定时任务...")

	// 示例：记录日志到文件
	f, err := os.OpenFile("service.log",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		p.logger.Error("无法打开日志文件:", err)
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	f.WriteString(fmt.Sprintf("[%s] Task executed\n", timestamp))
}

// Stop 服务停止时调用（由 service 框架调用）
func (p *program) Stop(s service.Service) error {
	p.logger.Info("服务正在停止...")

	// 通知 run() 方法退出
	close(p.exit)

	// 给清理工作预留时间
	time.Sleep(2 * time.Second)

	p.logger.Info("服务已停止")
	return nil
}

// ==================== 主程序入口 ====================

func main() {
	// 解析命令行参数
	action := flag.String("action", "", "install|uninstall|start|stop|restart|status")
	flag.Parse()

	// 服务配置
	svcConfig := &service.Config{
		Name:        serviceName,
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		Option: service.KeyValue{
			"DelayedAutoStart": true, // 系统完全启动后再运行
		},
	}

	// 创建服务程序实例
	prg := &program{}

	// 创建服务对象
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal("创建服务失败:", err)
	}

	// 初始化日志
	prg.logger, err = s.Logger(&logger{log: log.Default()})
	if err != nil {
		log.Fatal("初始化日志失败:", err)
	}

	// 无 action 参数时，直接运行服务（服务管理器会调用此分支）
	if *action == "" {
		err = s.Run()
		if err != nil {
			prg.logger.Error(err)
		}
		return
	}

	// 执行管理命令
	switch *action {
	case "install":
		if err = s.Install(); err != nil {
			log.Fatal("安装失败:", err)
		}
		log.Println("服务安装成功")

	case "uninstall":
		if err = s.Uninstall(); err != nil {
			log.Fatal("卸载失败:", err)
		}
		log.Println("服务卸载成功")

	case "start":
		if err = s.Start(); err != nil {
			log.Fatal("启动失败:", err)
		}
		log.Println("服务启动成功")

	case "stop":
		if err = s.Stop(); err != nil {
			log.Fatal("停止失败:", err)
		}
		log.Println("服务停止成功")

	case "restart":
		if err = s.Restart(); err != nil {
			log.Fatal("重启失败:", err)
		}
		log.Println("服务重启成功")

	case "status":
		status, err := s.Status()
		if err != nil {
			log.Fatal("获取状态失败:", err)
		}
		printStatus(status)

	default:
		log.Println("未知命令:", *action)
		log.Println("可用命令: install, uninstall, start, stop, restart, status")
	}
}

func printStatus(status service.Status) {
	switch status {
	case service.StatusRunning:
		log.Println("服务状态: 运行中")
	case service.StatusStopped:
		log.Println("服务状态: 已停止")
	default:
		log.Printf("服务状态: %d\n", status)
	}
}
```

### 3. 编译

```bash
# Windows
GOOS=windows GOARCH=amd64 go build -o myservice.exe

# Linux
GOOS=linux GOARCH=amd64 go build -o myservice

# macOS
GOOS=darwin GOARCH=amd64 go build -o myservice
```

### 4. 服务管理命令

**所有命令需要管理员权限（Windows）或 root（Linux）**

```bash
# 安装服务
myservice.exe -action=install

# 启动服务
myservice.exe -action=start

# 查看状态
myservice.exe -action=status

# 停止服务
myservice.exe -action=stop

# 重启服务
myservice.exe -action=restart

# 卸载服务
myservice.exe -action=uninstall
```

## 架构说明

### 核心组件

```
┌─────────────────────────────────────────┐
│           service.Service               │  <- kardianos/service 管理
│  (系统服务管理器 / systemd / launchd)   │
└─────────────────┬───────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────┐
│            program.Start()              │  <- 初始化，启动 goroutine
└─────────────────┬───────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────┐
│            program.run()                │  <- 业务逻辑主循环
│  ┌─────────┐  ┌─────────┐  ┌────────┐  │
│  │ ticker  │  │ sigChan │  │ p.exit │  │  <- 事件监听
│  └─────────┘  └─────────┘  └────────┘  │
└─────────────────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────┐
│            program.Stop()               │  <- 清理资源，优雅退出
└─────────────────────────────────────────┘
```

### 关键设计点

1. **Start() 不能阻塞**：必须在 goroutine 中运行实际逻辑
2. **exit channel**：用于内部通信，实现优雅停止
3. **信号捕获**：调试时支持 Ctrl+C，容器环境友好
4. **DelayedAutoStart**：避免与系统服务抢资源

## 自定义业务逻辑

修改 `doWork()` 方法即可：

```go
func (p *program) doWork() {
    // 示例 1：HTTP 服务器
    // go http.ListenAndServe(":8080", handler)

    // 示例 2：数据库同步
    // db.SyncData()

    // 示例 3：消息队列消费
    // consumer.Consume()

    // 示例 4：定时备份
    // backup.Run()
}
```

## 进阶配置

### 依赖其他服务

```go
svcConfig := &service.Config{
    Name:        serviceName,
    Dependencies: []string{
        "MSSQLSERVER",      // 依赖 SQL Server
        "EventLog",         // 依赖事件日志服务
    },
}
```

### 指定运行用户

```go
svcConfig := &service.Config{
    Name: serviceName,
    Option: service.KeyValue{
        "UserName": `Domain\Username`,
        "Password": "password",
    },
}
```

### 集成专业日志库

使用 `logrus` 或 `zap` 替代默认 log：

```go
import "github.com/sirupsen/logrus"

type logrusLogger struct {
    *logrus.Logger
}

func (l *logrusLogger) Error(v ...interface{}) error {
    l.Error(v...)
    return nil
}
// ... 实现其他接口方法
```

## 常见问题

### Q: 服务启动后立即停止？

确保 `Start()` 方法是非阻塞的：

```go
// 错误 ❌
func (p *program) Start(s service.Service) error {
    return p.run()  // 阻塞了！
}

// 正确 ✅
func (p *program) Start(s service.Service) error {
    go p.run()      // 在 goroutine 中运行
    return nil
}
```

### Q: 如何调试服务代码？

直接运行不带 `-action` 参数即可：

```bash
# 普通运行（非服务模式）
./myservice

# 可以看到控制台输出，支持 Ctrl+C 退出
```

### Q: 服务日志在哪里看？

Windows 事件查看器：
- 运行 `eventvwr.msc`
- Windows 日志 → 应用程序
- 来源：MyGoService

或查看程序目录下的 `service.log` 文件。

### Q: 如何设置开机启动？

```go
svcConfig := &service.Config{
    Name: serviceName,
    // Windows: 自动启动
    // Linux: systemd enable
    // macOS: launchctl load -w
}
```

安装后默认就是开机启动（Windows 的 Automatic 类型）。

## 参考

- [kardianos/service GitHub](https://github.com/kardianos/service)
- [Go Windows Service 文档](https://pkg.go.dev/golang.org/x/sys/windows/svc)
