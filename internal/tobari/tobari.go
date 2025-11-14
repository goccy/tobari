package tobari

import (
	"bytes"
	"fmt"
	"io"
	"runtime"
	"sync"
)

func ClearCounters() {
	entryMapMu.Lock()
	gMapMu.Lock()
	blockMapMu.Lock()

	entryMap = make(map[string]*TraceEntry)
	gMap = make(map[uint64]*TraceG)
	blockMap = make(map[string]*TraceBlock)

	blockMapMu.Unlock()
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
	_, _ = fmt.Fprintf(w, renderMap(mode, mergeMap))
}

func WriteCoverProfileByName(name string, mode Mode, w io.Writer) {
	entryMapMu.RLock()
	defer entryMapMu.RUnlock()

	for _, e := range entryMap {
		if e.Name != name {
			continue
		}
		_, _ = fmt.Fprintf(w, renderMap(mode, e.CoverprofileMap()))
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
			e = &TraceEntry{
				Name: name,
				Root: newTraceG(),
			}
			setEntry(entryID, e)
		}
		setG(gid, e.Root)
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
	Name string
	Root *TraceG
}

func (e *TraceEntry) CoverprofileMap() map[string]string {
	coverMap := createCoverprofileMapByMeta()
	e.Root.coverprofile(coverMap)
	return coverMap
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
	Blocks   []*TraceBlock
	Children []*TraceG
	blockMap map[int]*TraceBlock
	mu       sync.RWMutex
}

func newTraceG() *TraceG {
	return &TraceG{
		blockMap: make(map[int]*TraceBlock),
	}
}

func (g *TraceG) linkG(child *TraceG) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.Children = append(g.Children, child)
}

func (g *TraceG) addBlock(b *TraceBlock) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.Blocks = append(g.Blocks, b)
	g.blockMap[b.BlockIdx] = b
}

func (g *TraceG) hasBlock(blockIdx int) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, exists := g.blockMap[blockIdx]
	return exists
}

func (g *TraceG) coverprofile(coverMap map[string]string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, block := range g.Blocks {
		block.coverprofile(coverMap)
	}
	for _, child := range g.Children {
		child.coverprofile(coverMap)
	}
}

type TraceBlock struct {
	FileName   string
	BlockIdx   int
	NumStmts   int
	Start      Pos
	End        Pos
	CounterMap map[uint64]*TraceCounter
}

type TraceCounter struct {
	PGID    uint64
	GID     uint64
	Counter uint64
}

func (b *TraceBlock) coverprofile(coverMap map[string]string) {
	var sum uint64
	for _, c := range b.CounterMap {
		sum += c.Counter
	}
	coverMap[blockID(b.FileName, b.BlockIdx)] = fmt.Sprintf(
		"%s:%d.%d,%d.%d %d %d",
		b.FileName,
		b.Start.Line, b.Start.Col,
		b.End.Line, b.End.Col,
		b.NumStmts,
		sum,
	)
}

var (
	gidFnOnce  sync.Once
	gidFn      func() uint64
	entryMap   = make(map[string]*TraceEntry)
	entryMapMu sync.RWMutex
	gMap       = make(map[uint64]*TraceG)
	gMapMu     sync.RWMutex
	blockMap   = make(map[string]*TraceBlock)
	blockMapMu sync.RWMutex
	mdMu       sync.RWMutex
	mds        []*Metadata
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

func Trace(fileName, mode string, pgid, gid uint64, blockIdx, startLine, endLine, startCol, endCol, numStmts int) {
	g := getG(gid)
	if g == nil {
		g = newTraceG()
		setG(gid, g)
		if parent := getG(pgid); parent != nil {
			parent.linkG(g)
		}
	}
	block := getBlockWithCount(fileName, pgid, gid, blockIdx, startLine, endLine, startCol, endCol, numStmts)
	if !g.hasBlock(blockIdx) {
		g.addBlock(block)
	}
}

func getBlockWithCount(fileName string, pgid, gid uint64, blockIdx, startLine, endLine, startCol, endCol, numStmts int) *TraceBlock {
	blockMapMu.Lock()
	defer blockMapMu.Unlock()

	bid := blockID(fileName, blockIdx)
	if b, exists := blockMap[bid]; exists {
		counter, exists := b.CounterMap[gid]
		if !exists {
			b.CounterMap[gid] = &TraceCounter{
				PGID:    pgid,
				GID:     gid,
				Counter: 1,
			}
		} else {
			counter.Counter++
		}
		return b
	}

	block := &TraceBlock{
		FileName: fileName,
		BlockIdx: blockIdx,
		NumStmts: numStmts,
		Start: Pos{
			Line: startLine,
			Col:  startCol,
		},
		End: Pos{
			Line: endLine,
			Col:  endCol,
		},
		CounterMap: map[uint64]*TraceCounter{
			gid: &TraceCounter{
				PGID:    pgid,
				GID:     gid,
				Counter: 1,
			},
		},
	}
	blockMap[bid] = block
	return block
}

type Metadata struct {
	FileName string
	Blocks   []*Block
}

type Block struct {
	Start    Pos
	End      Pos
	NumStmts int
}

func AddCoverMeta(md Metadata) bool {
	mdMu.Lock()
	defer mdMu.Unlock()

	mds = append(mds, &md)
	return true
}

func createCoverprofileMapByMeta() map[string]string {
	mdMu.RLock()
	defer mdMu.RUnlock()

	ret := make(map[string]string)
	for _, md := range mds {
		for idx, block := range md.Blocks {
			ret[blockID(md.FileName, idx)] = fmt.Sprintf(
				"%s:%d.%d,%d.%d %d 0",
				md.FileName,
				block.Start.Line, block.Start.Col,
				block.End.Line, block.End.Col,
				block.NumStmts,
			)
		}
	}
	return ret
}

func renderMap(mode Mode, coverMap map[string]string) string {
	mdMu.RLock()

	var keys []string
	for _, md := range mds {
		for idx := range md.Blocks {
			keys = append(keys, blockID(md.FileName, idx))
		}
	}

	mdMu.RUnlock()

	b := bytes.NewBuffer([]byte(fmt.Sprintf("mode: %s\n", mode)))
	for _, key := range keys {
		_, _ = fmt.Fprintf(b, coverMap[key]+"\n")
	}
	return b.String()
}

func blockID(fileName string, blockIdx int) string {
	return fmt.Sprintf("%s:%d", fileName, blockIdx)
}
