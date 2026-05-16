package main

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
)

type Job func() error

type Queue struct {
	workers   int
	jobChan   chan Job
	wg        sync.WaitGroup
	stats     Stats
	stopOnce  sync.Once
	stopChan  chan struct{}
	submitted int32
	completed int32
	failed    int32
	panicked  int32
}

type Stats struct {
	Submitted int32
	Completed int32
	Failed    int32
	Panicked  int32
}

func NewQueue(workers int) *Queue {
	q := &Queue{
		workers:  workers,
		jobChan:  make(chan Job),
		stopChan: make(chan struct{}),
	}
	q.startWorkers()
	return q
}

func (q *Queue) startWorkers() {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for {
		select {
		case job, ok := <-q.jobChan:
			if !ok {
				return
			}
			q.executeJob(job)
		case <-q.stopChan:
			return
		}
	}
}
func (q *Queue) executeJob(job Job) {
	defer atomic.AddInt32(&q.completed, 1)
	defer func() {
		if r := recover(); r != nil {
			atomic.AddInt32(&q.panicked, 1)
			// log the panic as required by spec
			fmt.Fprintf(os.Stderr, "panic recovered: %v\n", r)
			os.Stderr.Sync()
		}
	}()
	err := job()
	if err != nil {
		atomic.AddInt32(&q.failed, 1)
	}
}


func (q *Queue) Submit(j Job) {
	atomic.AddInt32(&q.submitted, 1)
	select {
	case q.jobChan <- j:
	case <-q.stopChan:
		// queue closed, discard job
	}
}

func (q *Queue) Wait() {
	q.stopOnce.Do(func() {
		close(q.stopChan)
		close(q.jobChan)
	})
	q.wg.Wait()
	// populate stats struct
	q.stats = Stats{
		Submitted: atomic.LoadInt32(&q.submitted),
		Completed: atomic.LoadInt32(&q.completed),
		Failed:    atomic.LoadInt32(&q.failed),
		Panicked:  atomic.LoadInt32(&q.panicked),
	}
}

func (q *Queue) Stats() Stats {
	return q.stats
}

func main() {
	q := NewQueue(4)
	for i := 1; i <= 100; i++ {
		i := i // capture
		q.Submit(func() error {
			if i%7 == 0 {
				panic("panic on every 7th")
			}
			if i%5 == 0 {
				return fmt.Errorf("error on every 5th")
			}
			return nil
		})
	}
	q.Wait()
	s := q.Stats()
	fmt.Printf("submitted=%d completed=%d failed=%d panicked=%d\n",
		s.Submitted, s.Completed, s.Failed, s.Panicked)
}
