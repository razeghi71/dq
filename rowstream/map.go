package rowstream

import (
	"runtime"
	"sync"

	"github.com/razeghi71/dq/table"
)

// MapFunc is a pure row-local transformation. Returning keep=false drops the
// row, matching filter semantics.
type MapFunc func(Row) (Row, bool, error)

type ParallelOptions struct {
	Workers int
	Buffer  int
}

func DefaultParallelOptions() ParallelOptions {
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	return ParallelOptions{Workers: workers, Buffer: workers * 4}
}

func normalizeParallelOptions(opts ParallelOptions) ParallelOptions {
	if opts.Workers < 1 {
		opts.Workers = 1
	}
	if opts.Buffer < opts.Workers {
		opts.Buffer = opts.Workers
	}
	return opts
}

func identityMap(row Row) (Row, bool, error) {
	return row, true, nil
}

// Map applies fn serially while preserving the Stream contract.
func Map(input Stream, schema table.Schema, fn MapFunc) Stream {
	if fn == nil {
		fn = identityMap
	}
	return &mapStream{input: input, schema: schema, fn: fn}
}

type mapStream struct {
	input  Stream
	schema table.Schema
	fn     MapFunc
}

func (s *mapStream) Schema() table.Schema { return s.schema }

func (s *mapStream) Next() (Row, bool, error) {
	for {
		row, ok, err := s.input.Next()
		if err != nil || !ok {
			return row, ok, err
		}
		out, keep, err := s.fn(row)
		if err != nil {
			return nil, false, err
		}
		if keep {
			return out, true, nil
		}
	}
}

func (s *mapStream) Close() error {
	return s.input.Close()
}

// ParallelMapOrdered applies fn with bounded parallelism and emits kept rows in
// the same order as a serial Map. Runtime errors are also observed in input-row
// order, even when later workers finish first.
func ParallelMapOrdered(input Stream, schema table.Schema, fn MapFunc, opts ParallelOptions) Stream {
	opts = normalizeParallelOptions(opts)
	if opts.Workers == 1 {
		return Map(input, schema, fn)
	}
	if fn == nil {
		fn = identityMap
	}
	return &parallelMapStream{
		input:   input,
		schema:  schema,
		fn:      fn,
		opts:    opts,
		done:    make(chan struct{}),
		pending: make(map[int64]parallelResult),
	}
}

type parallelJob struct {
	seq int64
	row Row
}

type parallelResult struct {
	seq     int64
	row     Row
	keep    bool
	err     error
	release bool
}

type parallelMapStream struct {
	input  Stream
	schema table.Schema
	fn     MapFunc
	opts   ParallelOptions

	startOnce sync.Once
	closeOnce sync.Once
	closeErr  error

	done    chan struct{}
	jobs    chan parallelJob
	results chan parallelResult
	slots   chan struct{}

	pending map[int64]parallelResult
	nextSeq int64
}

func (s *parallelMapStream) Schema() table.Schema { return s.schema }

func (s *parallelMapStream) Next() (Row, bool, error) {
	s.start()
	for {
		if result, ok := s.pending[s.nextSeq]; ok {
			delete(s.pending, s.nextSeq)
			row, keep, err := s.consume(result)
			if err != nil || keep {
				return row, keep, err
			}
			continue
		}

		result, ok := <-s.results
		if !ok {
			return nil, false, nil
		}
		if result.seq != s.nextSeq {
			s.pending[result.seq] = result
			continue
		}
		row, keep, err := s.consume(result)
		if err != nil || keep {
			return row, keep, err
		}
	}
}

func (s *parallelMapStream) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.input != nil {
			s.closeErr = s.input.Close()
		}
	})
	return s.closeErr
}

func (s *parallelMapStream) start() {
	s.startOnce.Do(func() {
		s.jobs = make(chan parallelJob, s.opts.Buffer)
		s.results = make(chan parallelResult, s.opts.Buffer)
		s.slots = make(chan struct{}, s.opts.Buffer)
		for i := 0; i < s.opts.Buffer; i++ {
			s.slots <- struct{}{}
		}

		var wg sync.WaitGroup
		wg.Add(s.opts.Workers + 1)
		go s.readJobs(&wg)
		for i := 0; i < s.opts.Workers; i++ {
			go s.runWorker(&wg)
		}
		go func() {
			wg.Wait()
			close(s.results)
		}()
	})
}

func (s *parallelMapStream) readJobs(wg *sync.WaitGroup) {
	defer wg.Done()
	defer close(s.jobs)

	var seq int64
	for {
		select {
		case <-s.done:
			return
		case <-s.slots:
		}

		row, ok, err := s.input.Next()
		if err != nil {
			s.releaseSlot()
			s.sendResult(parallelResult{seq: seq, err: err})
			return
		}
		if !ok {
			s.releaseSlot()
			return
		}

		job := parallelJob{seq: seq, row: cloneRow(row)}
		seq++
		select {
		case s.jobs <- job:
		case <-s.done:
			s.releaseSlot()
			return
		}
	}
}

func (s *parallelMapStream) runWorker(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-s.done:
			return
		case job, ok := <-s.jobs:
			if !ok {
				return
			}
			row, keep, err := s.fn(job.row)
			if !s.sendResult(parallelResult{seq: job.seq, row: row, keep: keep, err: err, release: true}) {
				s.releaseSlot()
				return
			}
		}
	}
}

func (s *parallelMapStream) sendResult(result parallelResult) bool {
	select {
	case s.results <- result:
		return true
	case <-s.done:
		return false
	}
}

func (s *parallelMapStream) consume(result parallelResult) (Row, bool, error) {
	if result.release {
		s.releaseSlot()
	}
	s.nextSeq++
	if result.err != nil {
		return nil, false, result.err
	}
	if result.keep {
		return result.row, true, nil
	}
	return nil, false, nil
}

func (s *parallelMapStream) releaseSlot() {
	select {
	case s.slots <- struct{}{}:
	case <-s.done:
	}
}

func cloneRow(row Row) Row {
	if row == nil {
		return nil
	}
	out := make(Row, len(row))
	copy(out, row)
	return out
}
