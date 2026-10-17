package receiver

import (
	"sort"
	"strings"
	"sync"

	ocs7501 "github.com/nontechno/ocs.7501"
)

// Record is the current status of one id.
type Record struct {
	// Report holds exactly the keys of the last report for this id.
	ocs7501.Report

	// EffectiveApp is Report.App, or, if empty, the App of the nearest
	// ancestor record that has one (the root counts as the last ancestor).
	// It is computed when the record is read.
	EffectiveApp string

	// Seq orders updates: a larger value is a more recent update.
	Seq uint64
}

// Store holds the records of one terminal. Records belong to the terminal,
// not to a screen: switching between the main and alternate screen has no
// effect on them.
type Store struct {
	mu   sync.Mutex
	max  int
	seq  uint64
	recs map[string]*entry
}

type entry struct {
	r   ocs7501.Report
	seq uint64
}

// NewStore returns an empty Store that keeps at most maxRecords records. When
// it is full, the least recently updated record is evicted to make room. A
// value of 0 or less means ocs7501.DefaultRecords (256); values below
// ocs7501.MinRecords (64) are raised to it.
func NewStore(maxRecords int) *Store {
	switch {
	case maxRecords <= 0:
		maxRecords = ocs7501.DefaultRecords
	case maxRecords < ocs7501.MinRecords:
		maxRecords = ocs7501.MinRecords
	}
	return &Store{max: maxRecords, recs: make(map[string]*entry)}
}

// Handle processes the body of one OSC sequence, as delivered by the
// terminal's escape parser: everything between "ESC ]" and the terminator,
// e.g. "7501;state=working:app=cargo". Bodies that are not OSC 7501 or are
// invalid are ignored.
//
//   - For a status report it applies the report; changed says whether the
//     store changed.
//   - For a feature query it returns reply, the bytes to write back to the
//     program (the terminal accepts the default states and kinds). The reply
//     is the only thing a terminal ever sends back.
func (s *Store) Handle(body string) (reply string, changed bool) {
	m, err := ocs7501.ParseBody(body)
	if err != nil {
		return "", false
	}
	if m.Features != nil {
		return ocs7501.DefaultReply, false
	}
	return "", s.Apply(*m.Report)
}

// Apply applies a report. A report that is not legal (see Report.Validate)
// is ignored. It returns whether the store changed.
//
//   - StateClear removes the addressed record and all its descendants; with
//     no id it removes every record.
//   - Any other state replaces the record for the id completely.
func (s *Store) Apply(r ocs7501.Report) bool {
	if r.Validate() != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if r.State == ocs7501.StateClear {
		changed := false
		for id := range s.recs {
			if isSelfOrDescendant(id, r.ID) {
				delete(s.recs, id)
				changed = true
			}
		}
		return changed
	}

	r = clone(r)
	old, existed := s.recs[r.ID]
	if !existed && len(s.recs) >= s.max {
		s.evictOldest()
	}
	s.seq++
	s.recs[r.ID] = &entry{r: r, seq: s.seq}
	return !existed || !equal(old.r, r)
}

// ProgramEnded must be called when the process attached to the terminal
// exits, and when the shell starts a new prompt (OSC 133 ; A). It drops the
// working, blocked and idle records and keeps done and error records, which
// must survive both. It returns whether anything was dropped.
func (s *Store) ProgramEnded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, e := range s.recs {
		switch e.r.State {
		case ocs7501.StateWorking, ocs7501.StateBlocked, ocs7501.StateIdle:
			delete(s.recs, id)
			changed = true
		}
	}
	return changed
}

// Reset removes every record. Call it on a full terminal reset (RIS). A soft
// reset (DECSTR) must not call it.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs = make(map[string]*entry)
}

// Get returns the record for id ("" is the root).
func (s *Store) Get(id string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.recs[id]
	if !ok {
		return Record{}, false
	}
	return s.record(id, e), true
}

// Records returns a copy of all records, ordered by id (the root first).
func (s *Store) Records() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.recs))
	for id := range s.recs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Record, len(ids))
	for i, id := range ids {
		out[i] = s.record(id, s.recs[id])
	}
	return out
}

// Len returns the number of records.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.recs)
}

// record builds the public view of e; s.mu must be held.
func (s *Store) record(id string, e *entry) Record {
	return Record{Report: clone(e.r), EffectiveApp: s.effectiveApp(id), Seq: e.seq}
}

// effectiveApp walks from id up to the root and returns the first app found.
func (s *Store) effectiveApp(id string) string {
	for {
		if e, ok := s.recs[id]; ok && e.r.App != "" {
			return e.r.App
		}
		if id == "" {
			return ""
		}
		if i := strings.LastIndexByte(id, '/'); i >= 0 {
			id = id[:i]
		} else {
			id = ""
		}
	}
}

func (s *Store) evictOldest() {
	var oldID string
	var oldSeq uint64
	first := true
	for id, e := range s.recs {
		if first || e.seq < oldSeq {
			oldID, oldSeq, first = id, e.seq, false
		}
	}
	if !first {
		delete(s.recs, oldID)
	}
}

// isSelfOrDescendant reports whether id is anc or lies below it. The root
// ("") is an ancestor of every id.
func isSelfOrDescendant(id, anc string) bool {
	return anc == "" || id == anc || strings.HasPrefix(id, anc+"/")
}

func clone(r ocs7501.Report) ocs7501.Report {
	if r.Progress != nil {
		n := *r.Progress
		r.Progress = &n
	}
	return r
}

func equal(a, b ocs7501.Report) bool {
	if (a.Progress == nil) != (b.Progress == nil) || (a.Progress != nil && *a.Progress != *b.Progress) {
		return false
	}
	a.Progress, b.Progress = nil, nil
	return a == b
}
