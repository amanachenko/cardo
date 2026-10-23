package madeup

import (
	"context"
	"encoding/json"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"time"
)

// Sink receives what the made-up organization sends: each hook payload with the time it stands
// for, and OTLP/HTTP JSON requests for logs and metrics.
type Sink interface {
	Hook(at time.Time, body []byte) error
	Logs(body []byte) error
	Metrics(body []byte) error
}

// Most log records or metric points in one OTLP request. A session that makes more is split
// across several requests with the same resource.
const (
	logsPerRequest   = 2000
	pointsPerRequest = 1000
)

// Generate writes every person's history to sink and returns the answer key. People are written
// in parallel by up to workers goroutines; each person's events depend only on the seed, so the
// key and the set of requests sent are the same however many there are.
func Generate(ctx context.Context, o *Org, sink Sink, workers int) (*Key, error) {
	tallies := make([]*tally, len(o.People))
	jobs := make(chan int)
	var (
		wg    sync.WaitGroup
		once  sync.Once
		first error
	)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	fail := func(err error) {
		once.Do(func() { first = err; cancel() })
	}
	for range max(1, workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				t, err := o.writePerson(ctx, o.People[i], sink)
				if err != nil {
					fail(err)
					continue
				}
				tallies[i] = t
			}
		}()
	}
feed:
	for i := range o.People {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if first != nil {
		return nil, first
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return buildKey(o, tallies), nil
}

// writePerson writes one person's whole history, day by day.
func (o *Org) writePerson(ctx context.Context, p *Person, sink Sink) (*tally, error) {
	t := newTally(o.Preset.Stories)
	b := &batch{sink: sink}
	ws := &weekState{}
	for day := range o.Days {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r := newRand(o.Seed, uint64(day+1)<<40|uint64(p.Index))
		for _, pl := range o.schedule(r, p, day) {
			if week := int(pl.at.Sub(o.Start)/(24*time.Hour))/7 + 1; week != ws.week {
				ws = &weekState{week: week}
			}
			var budget float64
			switch pl.kind {
			case overnight:
				budget = logUniform(r, 12, 36)
			case gaming:
				budget = logUniform(r, 0.03, 0.08)
			default:
				perSession := p.MonthlySpend / p.MonthlySessions
				budget = perSession * logNormal(r, 1, 0.6) / math.Exp(0.18)
			}
			s := writeSession(o, p, r, pl.kind, pl.at, budget, ws)
			if !s.last.Before(o.End()) {
				continue // it would run past the end of the history
			}
			t.add(&s.tally, p)
			if err := b.add(s); err != nil {
				return nil, err
			}
		}
	}
	if err := b.flush(); err != nil {
		return nil, err
	}
	return t, nil
}

// planned is a session to write: when it starts, and what starts it.
type planned struct {
	at   time.Time
	kind sessionKind
}

// schedule gives a person's sessions on one day, in order.
func (o *Org) schedule(r *rand.Rand, p *Person, day int) []planned {
	if day < p.JoinDay || p.DaysOff[day] {
		return nil
	}
	date := o.Start.AddDate(0, 0, day)
	local := func(hours float64) time.Time {
		return date.Add(time.Duration((hours - p.Team.UTC) * float64(time.Hour)))
	}
	wd := date.Weekday()
	workday := wd != time.Saturday && wd != time.Sunday
	var out []planned
	if p.Gamer && workday && day/7+1 >= o.Preset.Stories.Gamer {
		// The script runs one /review session after another through the working day.
		at := local(9.5 + r.Float64())
		for range gamerSessionsMin + r.IntN(gamerSessionsMax-gamerSessionsMin+1) {
			out = append(out, planned{at, gaming})
			at = at.Add(time.Duration(logNormal(r, float64(4*time.Minute), 0.3)))
		}
	}
	if p.Overnight && wd >= time.Monday && wd <= time.Thursday && r.Float64() < 0.7 {
		out = append(out, planned{local(22 + 1.5*r.Float64()), overnight})
	}
	out = append(out, o.ownSessions(r, p, date, local)...)
	slices.SortFunc(out, func(a, b planned) int { return a.at.Compare(b.at) })
	return out
}

// ownSessions gives the sessions a person starts themselves on one day.
func (o *Org) ownSessions(r *rand.Rand, p *Person, date time.Time, local func(float64) time.Time) []planned {
	s := p.Style
	chance := s.ActiveDay
	if wd := date.Weekday(); wd == time.Saturday || wd == time.Sunday {
		chance = s.Weekend
	}
	if r.Float64() >= chance {
		return nil
	}
	perActiveDay := p.MonthlySessions / (21.7*s.ActiveDay + 8.7*s.Weekend)
	n := max(1, poisson(r, perActiveDay*logNormal(r, 1, 0.35)))

	var out []time.Time
	if s.Scripted {
		// A script runs its sessions one after another, in a few batches a day.
		batches := 1 + r.IntN(3)
		for b := range batches {
			at := local(8 + 12*r.Float64())
			for range n / batches {
				out = append(out, at)
				at = at.Add(time.Duration(logNormal(r, float64(90*time.Second), 0.7)))
			}
			if b == 0 {
				for range n % batches {
					out = append(out, at)
					at = at.Add(time.Duration(logNormal(r, float64(90*time.Second), 0.7)))
				}
			}
		}
	} else {
		for range n {
			var h float64
			switch x := r.Float64(); {
			case x < 0.45:
				h = 9 + 3*r.Float64()
			case x < 0.9:
				h = 13 + 5*r.Float64()
			default:
				h = 19 + 4*r.Float64()
			}
			out = append(out, local(h))
		}
	}
	plans := make([]planned, len(out))
	for i, at := range out {
		plans[i] = planned{at, worked}
	}
	return plans
}

// batch gathers one person's sessions into OTLP requests.
type batch struct {
	sink    Sink
	logs    []resourceLogs
	nlogs   int
	metrics []resourceMetrics
	npoints int
}

func (b *batch) add(s *session) error {
	for _, h := range s.hooks {
		if err := b.sink.Hook(h.at, h.body); err != nil {
			return err
		}
	}
	res := resource{s.resource}
	sc := scope{Name: logScope, Version: s.version}
	for rest := s.logs; len(rest) > 0; {
		n := min(len(rest), logsPerRequest-b.nlogs)
		b.logs = append(b.logs, resourceLogs{res, []scopeLogs{{sc, rest[:n]}}})
		b.nlogs += n
		rest = rest[n:]
		if b.nlogs >= logsPerRequest {
			if err := b.flushLogs(); err != nil {
				return err
			}
		}
	}
	points := 0
	for _, m := range s.points {
		points += len(m.Sum.DataPoints)
	}
	b.metrics = append(b.metrics, resourceMetrics{res, []scopeMetrics{{scope{Name: metricScope, Version: s.version}, s.points}}})
	b.npoints += points
	if b.npoints >= pointsPerRequest {
		return b.flushMetrics()
	}
	return nil
}

func (b *batch) flushLogs() error {
	if len(b.logs) == 0 {
		return nil
	}
	body, err := json.Marshal(logsRequest{b.logs})
	if err != nil {
		return err
	}
	b.logs, b.nlogs = nil, 0
	return b.sink.Logs(body)
}

func (b *batch) flushMetrics() error {
	if len(b.metrics) == 0 {
		return nil
	}
	body, err := json.Marshal(metricsRequest{b.metrics})
	if err != nil {
		return err
	}
	b.metrics, b.npoints = nil, 0
	return b.sink.Metrics(body)
}

func (b *batch) flush() error {
	if err := b.flushLogs(); err != nil {
		return err
	}
	return b.flushMetrics()
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
