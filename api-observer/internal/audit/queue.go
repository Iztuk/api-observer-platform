package audit

import (
	"context"
	"log"
	"os"
	"runtime/debug"
	"sync"
)

type Queue struct {
	jobs chan Job
	mu   sync.RWMutex
	done bool
	once sync.Once
}

func (j RequestJob) Evaluate() []Finding {
	findings := make([]Finding, 0)

	return findings
}

func (j ResponseJob) Evaluate() []Finding {
	findings := make([]Finding, 0)

	return findings
}

func NewQueue(size int) *Queue {
	return &Queue{
		jobs: make(chan Job, size),
	}
}

func (q *Queue) TryEnqueue(job Job) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.done {
		return false
	}

	if job.Request == nil && job.Response == nil {
		return false
	}

	select {
	case q.jobs <- job:
		return true
	default:
		return false
	}
}

func (q *Queue) StartWorkers(ctx context.Context, rs *RuleSet, count int, al, fl *log.Logger) *sync.WaitGroup {
	var wg sync.WaitGroup

	findingsFile, ok := fl.Writer().(*os.File)
	if !ok {
		al.Println(
			"findings logger is not writing directly to a file",
		)

		return &wg
	}

	findingsPath := findingsFile.Name()

	wg.Add(1)

	go func() {
		defer wg.Done()

		if err := WatchTimeIndexFile(
			ctx,
			findingsPath,
			256,
		); err != nil {
			al.Printf(
				"findings indexer stopped: %v",
				err,
			)
		}
	}()

	for i := range count {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()

			for job := range q.jobs {
				func() {
					defer func() {
						if r := recover(); r != nil {
							al.Printf(
								"audit worker %d panic: %v\n%s\n",
								workerID,
								r,
								debug.Stack(),
							)
						}
					}()

					findings, err := rs.Evaluate(job)
					if err != nil {
						al.Printf("an error occured while evaluating job: %v", err)
					}
					for _, finding := range findings {
						finding.Log(al, fl)
					}
				}()
			}

			al.Printf("audit worker %d queue closed", workerID)
		}(i)
	}

	return &wg
}

func (q *Queue) Close() {
	q.once.Do(func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		q.done = true
		close(q.jobs)
	})
}
