// Copyright The AIGW Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metadata

import (
	"context"
	"errors"
	"time"

	"github.com/envoyproxy/envoy/contrib/golang/common/go/api"
)

// Task 异步任务
type Task struct {
	HashKey string
	Method  string
	URL     string
	Body    []byte
	TraceId string
	Timeout time.Duration
}

// TaskHandler 任务处理器接口
type TaskHandler interface {
	HandleRequest(ctx context.Context, task *Task) error
}

// AsyncQueue 异步任务队列
type AsyncQueue struct {
	taskChan       chan *Task
	handler        TaskHandler
	defaultTimeout time.Duration
	workerCount    int
}

// NewAsyncQueue 创建新的异步队列
func NewAsyncQueue(queueSize, workerCount int, defaultTimeout time.Duration, handler TaskHandler) *AsyncQueue {
	queue := &AsyncQueue{
		taskChan:       make(chan *Task, queueSize),
		handler:        handler,
		defaultTimeout: defaultTimeout,
		workerCount:    workerCount,
	}

	// 启动工作协程
	for i := 0; i < workerCount; i++ {
		go queue.worker(i)
	}

	api.LogInfof("async queue started with %d workers, queue size=%d", workerCount, queueSize)
	return queue
}

// Dispatch 分发任务到队列
func (q *AsyncQueue) Dispatch(task *Task) error {
	if task.Timeout == 0 {
		task.Timeout = q.defaultTimeout
	}

	select {
	case q.taskChan <- task:
		return nil
	default:
		api.LogWarnf("async queue is full, dropping task: %s %s", task.Method, task.URL)
		return errors.New("queue is full")
	}
}

// worker 工作协程
func (q *AsyncQueue) worker(id int) {
	for task := range q.taskChan {
		q.processTask(task)
	}
}

// processTask 处理单个任务
func (q *AsyncQueue) processTask(task *Task) {
	ctx, cancel := context.WithTimeout(context.Background(), task.Timeout)
	defer cancel()

	if err := q.handler.HandleRequest(ctx, task); err != nil {
		api.LogWarnf("[TraceID: %s] async task failed: %s %s, err: %v",
			task.TraceId, task.Method, task.URL, err)
	} else {
		api.LogDebugf("[TraceID: %s] async task completed: %s %s",
			task.TraceId, task.Method, task.URL)
	}
}

// Close 关闭队列
func (q *AsyncQueue) Close() {
	close(q.taskChan)
}
