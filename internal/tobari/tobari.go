package tobari

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"runtime"
	"sync"
)

func ClearCounters() {
	entryMapMu.Lock()
	gMapMu.Lock()

	entryMap = make(map[string]*TraceEntry)
	gMap = make(map[uint64]*TraceG)

	gMapMu.Unlock()
	entryMapMu.Unlock()
}

type Mode string

const (
	SetMode    Mode = "set"
	CountMode  Mode = "count"
	AtomicMode Mode = "atomic"
)

func CoverProfileMap(mode Mode) map[string]string {
	entryMapMu.RLock()
	defer entryMapMu.RUnlock()

	ret := make(map[string]string)
	for _, e := range entryMap {
		ret[e.Name] = renderMap(mode, e.CoverprofileMap())
	}
	return ret
}

func WriteCoverProfile(mode Mode, w io.Writer) {
	entryMapMu.RLock()
	defer entryMapMu.RUnlock()

	mergeMap := make(map[string]string)
	for _, e := range entryMap {
		for k, v := range e.CoverprofileMap() {
			mergeMap[k] = v
		}
	}
	_, _ = fmt.Fprint(w, renderMap(mode, mergeMap))
}

func WriteCoverProfileByName(name string, mode Mode, w io.Writer) {
	entryMapMu.RLock()
	defer entryMapMu.RUnlock()

	for _, e := range entryMap {
		if e.Name != name {
			continue
		}
		_, _ = fmt.Fprint(w, renderMap(mode, e.CoverprofileMap()))
		return
	}
}

func Cover(fn func()) {
	cover("", fn)
}

func CoverWithName(name string, fn func()) {
	cover(name, fn)
}

func cover(name string, fn func()) {
	ch := make(chan struct{})
	_, file, line, _ := runtime.Caller(2)
	entryID := fmt.Sprintf("%s:%s:%d", name, file, line)
	go func() {
		gid := currentGID()
		e := getEntry(entryID)
		if e == nil {
			e = &TraceEntry{Name: name}
			setEntry(entryID, e)
		}
		root := newTraceG(gid)
		e.Roots = append(e.Roots, root)
		setG(gid, root)
		fn()
		ch <- struct{}{}
	}()
	<-ch
}

type Pos struct {
	Line int
	Col  int
}

type TraceEntry struct {
	Name  string
	Roots []*TraceG
}

func (e *TraceEntry) CoverprofileMap() map[string]string {
	newCoverprofileMap := make(map[string]string)
	allCoverprofileMapMu.RLock()
	maps.Copy(newCoverprofileMap, allCoverprofileMap)
	allCoverprofileMapMu.RUnlock()

	blockToCountMap := make(map[string]int)
	for _, root := range e.Roots {
		root.blockToCountMap(blockToCountMap)
	}
	for blockID, count := range blockToCountMap {
		block := getBlock(blockID)
		if block == nil {
			continue
		}
		newCoverprofileMap[blockID] = fmt.Sprintf(
			"%s:%d.%d,%d.%d %d %d",
			block.FileName,
			block.Start.Line, block.Start.Col,
			block.End.Line, block.End.Col,
			block.NumStmts,
			count,
		)
	}
	return newCoverprofileMap
}

func getEntry(id string) *TraceEntry {
	entryMapMu.RLock()
	defer entryMapMu.RUnlock()
	return entryMap[id]
}

func setEntry(id string, e *TraceEntry) {
	entryMapMu.Lock()
	entryMap[id] = e
	entryMapMu.Unlock()
}

type TraceG struct {
	ID              uint64
	BlockCounterMap map[string]int
	Children        []*TraceG
	mu              sync.RWMutex
}

func newTraceG(gid uint64) *TraceG {
	return &TraceG{
		ID:              gid,
		BlockCounterMap: make(map[string]int),
	}
}

func (g *TraceG) addCounter(blockID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.BlockCounterMap[blockID]++
}

func (g *TraceG) linkG(child *TraceG) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.Children = append(g.Children, child)
}

func (g *TraceG) blockToCountMap(blockToCountMap map[string]int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for blockID, count := range g.BlockCounterMap {
		blockToCountMap[blockID] += count
	}
	for _, child := range g.Children {
		child.blockToCountMap(blockToCountMap)
	}
}

var (
	gidFnOnce              sync.Once
	gidFn                  func() uint64
	entryMap               = make(map[string]*TraceEntry)
	entryMapMu             sync.RWMutex
	gMap                   = make(map[uint64]*TraceG)
	gMapMu                 sync.RWMutex
	blockMap               = make(map[string]*Block)
	blockMapMu             sync.RWMutex
	mdMu                   sync.RWMutex
	mds                    []*Metadata
	allCoverprofileMap     = make(map[string]string)
	allCoverprofileMapKeys []string
	allCoverprofileMapMu   sync.RWMutex
)

func SetGIDFunc(fn func() uint64) bool {
	gidFnOnce.Do(func() {
		gidFn = fn
	})
	return true
}

func currentGID() uint64 {
	if gidFn == nil {
		return 0
	}
	return gidFn()
}

func getG(gid uint64) *TraceG {
	gMapMu.RLock()
	defer gMapMu.RUnlock()
	return gMap[gid]
}

func setG(gid uint64, g *TraceG) {
	gMapMu.Lock()
	gMap[gid] = g
	gMapMu.Unlock()
}

func getBlock(blockID string) *Block {
	blockMapMu.RLock()
	defer blockMapMu.RUnlock()

	return blockMap[blockID]
}

func Trace(fileName string, pgid, gid uint64, blockIdx, startLine, endLine, startCol, endCol, numStmts int) {
	g := getG(gid)
	if g == nil {
		g = newTraceG(gid)
		setG(gid, g)
		if parent := getG(pgid); parent != nil {
			parent.linkG(g)
		}
	}
	g.addCounter(blockID(fileName, blockIdx))
}

type Metadata struct {
	FileName string
	Funcs    []*Function
}

type Function struct {
	Name   string
	Blocks []*Block
	Deps   []string
}

type Block struct {
	FileName string
	Idx      int
	Start    Pos
	End      Pos
	NumStmts int
}

func AddCoverMeta(md Metadata) bool {
	allCoverprofileMapMu.Lock()
	for _, fn := range md.Funcs {
		for _, block := range fn.Blocks {
			bid := blockID(md.FileName, block.Idx)
			block.FileName = md.FileName

			blockMapMu.Lock()
			blockMap[bid] = block
			blockMapMu.Unlock()

			allCoverprofileMap[bid] = fmt.Sprintf(
				"%s:%d.%d,%d.%d %d 0",
				md.FileName,
				block.Start.Line, block.Start.Col,
				block.End.Line, block.End.Col,
				block.NumStmts,
			)
			allCoverprofileMapKeys = append(allCoverprofileMapKeys, bid)
		}
	}
	allCoverprofileMapMu.Unlock()

	mdMu.Lock()
	mds = append(mds, &md)
	mdMu.Unlock()

	return true
}

func renderMap(mode Mode, coverMap map[string]string) string {
	b := bytes.NewBuffer([]byte(fmt.Sprintf("mode: %s\n", mode)))
	for _, key := range allCoverprofileMapKeys {
		_, _ = fmt.Fprint(b, coverMap[key]+"\n")
	}
	return b.String()
}

func blockID(fileName string, blockIdx int) string {
	return fmt.Sprintf("%s:%d", fileName, blockIdx)
}
