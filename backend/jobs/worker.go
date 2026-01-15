package jobs

import (
	"fmt"
	"sync"
)

// Job represents a unit of work
type Job struct {
	ID      string
	Execute func() error
}

// WorkerPool manages a pool of workers for async job processing
type WorkerPool struct {
	workerCount int
	jobQueue    chan Job
	wg          sync.WaitGroup
	stopOnce    sync.Once
	done        chan struct{}
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(workerCount int) *WorkerPool {
	pool := &WorkerPool{
		workerCount: workerCount,
		jobQueue:    make(chan Job, workerCount*2), // Buffer size = 2x workers
		done:        make(chan struct{}),
	}

	// Start workers
	for i := 0; i < workerCount; i++ {
		pool.wg.Add(1)
		go pool.worker(i)
	}

	fmt.Printf("Started worker pool with %d workers\n", workerCount)
	return pool
}

// worker processes jobs from the queue
func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()

	fmt.Printf("Worker %d started\n", id)

	for {
		select {
		case job, ok := <-p.jobQueue:
			if !ok {
				fmt.Printf("Worker %d stopped (channel closed)\n", id)
				return
			}

			fmt.Printf("Worker %d processing job %s\n", id, job.ID)
			if err := job.Execute(); err != nil {
				fmt.Printf("Worker %d job %s failed: %v\n", id, job.ID, err)
			} else {
				fmt.Printf("Worker %d job %s completed\n", id, job.ID)
			}

		case <-p.done:
			fmt.Printf("Worker %d stopped (shutdown signal)\n", id)
			return
		}
	}
}

// Submit adds a job to the queue
func (p *WorkerPool) Submit(job Job) error {
	select {
	case p.jobQueue <- job:
		fmt.Printf("Job %s submitted to queue\n", job.ID)
		return nil
	case <-p.done:
		return fmt.Errorf("worker pool is shutting down")
	}
}

// Stop gracefully shuts down the worker pool
func (p *WorkerPool) Stop() {
	p.stopOnce.Do(func() {
		fmt.Println("Stopping worker pool...")
		close(p.done)
		close(p.jobQueue)
		p.wg.Wait()
		fmt.Println("Worker pool stopped")
	})
}

// QueueSize returns the current number of jobs in queue
func (p *WorkerPool) QueueSize() int {
	return len(p.jobQueue)
}
