package v1

import (
	"bufio"
	"cloudiac/configs"
	"cloudiac/runner"
	"cloudiac/runner/ws"
	"cloudiac/utils/logs"
	"context"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// TaskLogFollow 读取 tas log 并 follow, 直到任务退出
func TaskLogFollow(c *gin.Context) {
	task := runner.CommitedTask{
		TemplateId: c.Query("templateId"),
		TaskId:     c.Query("taskId"),
	}

	logger := logger.WithField("taskId", task.TaskId)
	wsConn, err := ws.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Warnln(err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer wsConn.Close()

	// 通知处理进程，对端主动断开了连接
	peerClosed := make(chan struct{})
	go func() {
		for {
			// 通过调用 ReadMessage() 来检测对端是否断开连接，
			// 如果对端关闭连接，该调用会返回 error，其他消息我们忽略
			_, _, err := wsConn.ReadMessage()
			if err != nil {
				close(peerClosed)
				if !websocket.IsUnexpectedCloseError(err) {
					logger.Debugf("read ws message: %v", err)
				}
				return
			}
		}
	}()

	sendCloseMessage := func(code int, text string) {
		message := websocket.FormatCloseMessage(code, "")
		wsConn.WriteControl(websocket.CloseMessage, message, time.Now().Add(time.Second))
	}

	if err := doFollowTaskLog(wsConn, &task, 0, peerClosed); err != nil {
		logger.Errorf("doFollowTaskLog error: %v", err)
		sendCloseMessage(websocket.CloseInternalServerErr, err.Error())
	} else {
		sendCloseMessage(websocket.CloseNormalClosure, "")
	}
}

func doFollowTaskLog(wsConn *websocket.Conn, task *runner.CommitedTask, offset int64, closed <-chan struct{}) error {
	logger := logger.WithField("func", "doFollowTaskLog").WithField("taskId", task.TaskId)

	var (
		taskExitChan = make(chan error)
	)

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()

	conf := configs.Get()
	logPath := filepath.Join(conf.Runner.LogBasePath, task.TemplateId, task.TaskId, runner.ContainerLogFileName)
	contentChan, readErrChan := followFile(ctx, logPath, offset)

	// 监听任务退出
	go func() {
		defer close(taskExitChan)

		_, err := task.Wait(ctx)
		taskExitChan <- err
	}()

	for {
		select {
		case content := <-contentChan:
			if err := wsConn.WriteMessage(websocket.TextMessage, content); err != nil {
				logger.Errorf("write message error: %v", err)
				return err
			}
		case err := <-readErrChan:
			if err != nil {
				logger.Errorf("read content error: %v", err)
				return err
			}
		case err := <-taskExitChan:
			if err != nil {
				logger.Errorf("wait task error: %v", err)
			}
			return err
		case <-closed:
			logger.Debugf("connection closed")
			return nil
		}
	}
}

// 读取文件内容并 follow，直到 ctx 被 cancel
// return: 两个 chan，一个用于返回文件内容，一个用于返回 err，chan 在函数退出时会被关闭，所以 chan 会读到 nil
func followFile(ctx context.Context, path string, offset int64) (<-chan []byte, <-chan error) {
	logger := logs.Get().WithField("func", "followFile").WithField("path", path)

	var (
		contentChan = make(chan []byte)
		errChan     = make(chan error, 1)
	)

	logFp, err := os.Open(path)
	if err != nil {
		errChan <- err
		return contentChan, errChan
	}

	if offset != 0 {
		if _, err := logFp.Seek(offset, 0); err != nil {
			_ = logFp.Close()
			return contentChan, errChan
		}
	}

	go func() {
		defer func() {
			_ = logFp.Close()
			close(contentChan)
			close(errChan)
		}()

		reader := bufio.NewReader(logFp)
		for {
			content, err := reader.ReadBytes('\n')
			if len(content) > 0 {
				contentChan <- content
			}

			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					if err == io.EOF {
						// 读到了文件末尾，等待一下再进行下一次读取
						time.Sleep(time.Second)
						continue
					} else {
						errChan <- err
						return
					}
				}
			}
		}
	}()

	go func() {
		<-ctx.Done()
		logger.Debugf("context done, %v", ctx.Err())
		// 关闭文件，中断 Read()
		_ = logFp.Close()
	}()

	return contentChan, errChan
}
