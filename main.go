package main

import (
	"bufio"
	"bytes"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Edge - edge of word graph
type Edge struct {
	S         int
	WordCount int
	UnkCount  int
}

type DictBuilderPointer struct {
	NodeID  int
	Offset  int
	IsFinal bool
}

// PrefixTreeNode represents node in a prefix tree
type PrefixTreeNode struct {
	NodeID int
	Offset int
	Ch     rune
}

// PrefixTreePointer is partial information of edge
type PrefixTreePointer struct {
	ChildID int
	IsFinal bool
}

// PrefixTree is a Hash-based Prefix Tree for searching words
type PrefixTree map[PrefixTreeNode]PrefixTreePointer

type LineInput struct {
	lineNo    int
	textRunes []rune
}

func IsSpace(ch rune) bool {
	return ch == ' ' ||
		ch == '\n' ||
		ch == '\t' ||
		ch == '"' ||
		ch == '(' ||
		ch == ')' ||
		ch == '“' ||
		ch == '”'
}

func IsLatin(ch rune) bool {
	return (ch >= 'A' && ch <= 'Z') ||
		(ch >= 'a' && ch <= 'z')
}

// LoadDict is for loading a word list from file
func LoadDict(path string) (PrefixTree, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(b))

	lines := make([]string, 0)
	for scanner.Scan() {
		if line := scanner.Text(); len(line) != 0 {
			lines = append(lines, line)
		}
	}

	sort.Strings(lines)

	tab := make(PrefixTree)
	for i, line := range lines {
		rowNo := 0
		runes := []rune(line)
		len := len(runes)

		for j, ch := range runes {
			isFinal := ((j + 1) == len)
			node := PrefixTreeNode{rowNo, j, ch}

			if child, found := tab[node]; !found {
				tab[node] = PrefixTreePointer{i, isFinal}
				rowNo = i
			} else {
				rowNo = child.ChildID
			}
		}
	}

	return tab, nil
}

// LoadDefaultDict - loading default Thai dictionary
func LoadDefaultDict() (PrefixTree, error) {
	_, filename, _, _ := runtime.Caller(0)
	return LoadDict(path.Join(path.Dir(filename), "tdict-std.txt"))
}

func main() {
	// p := profile.Start(profile.CPUProfile, profile.ProfilePath("."))
	// defer p.Stop()

	var dictPath string
	flag.StringVar(&dictPath, "dix", "", "Dictionary path")
	flag.Parse()

	NewSegmenterWorker(dictPath).Run()
}

func NewSegmenterWorker(dictPath string) *SegmenterWorker {
	dict, err := LoadDict(dictPath)
	if err != nil {
		log.Fatal(err)
	}

	return &SegmenterWorker{
		dict: dict,
	}
}

type SegmenterWorker struct {
	dict PrefixTree

	lineInputCh chan LineInput
	result      Result
	done        chan struct{}

	wg   sync.WaitGroup
	once sync.Once
}

func (w *SegmenterWorker) StartWorker() {
	w.lineInputCh = make(chan LineInput, runtime.NumCPU())
	w.result = Result{
		out:    bufio.NewWriter(os.Stdout),
		result: make(map[int]string),
	}
	w.done = make(chan struct{})

	for wc := 0; wc < runtime.NumCPU(); wc++ {
		go func() {
			sm := Segmenter{
				dict: w.dict,
			}

			for {
				select {
				case lineInput := <-w.lineInputCh:
					result := strings.Join(sm.Segment(lineInput.textRunes), "|") + "\n"
					w.result.Set(lineInput.lineNo, result)
					w.wg.Done()
				case <-w.done:
					return
				}
			}

		}()
	}
}

func (w *SegmenterWorker) Run() {
	w.once.Do(w.StartWorker)

	b, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal("could not read input:", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(b))

	i := 0
	for scanner.Scan() {
		w.wg.Add(1)

		text := scanner.Text()

		w.lineInputCh <- LineInput{
			lineNo:    i,
			textRunes: []rune(text),
		}

		i++
	}

	w.wg.Wait()
	close(w.done)
	w.result.WriteOut()
}

type Result struct {
	out *bufio.Writer

	mu     sync.Mutex
	result map[int]string
}

func (r *Result) Set(lineNo int, line string) {
	r.mu.Lock()
	r.result[lineNo] = line
	r.mu.Unlock()
}

func (r *Result) WriteOut() {
	for i := 0; i < len(r.result); i++ {
		r.mu.Lock()
		r.out.WriteString(r.result[i])
		r.mu.Unlock()
	}
	r.out.Flush()
}

type Segmenter struct {
	dict     PrefixTree
	path     []Edge
	pointers []DictBuilderPointer
}

func (sm *Segmenter) Segment(textRunes []rune) []string {
	sm.BuildPath(textRunes)

	l := len(sm.path)
	tokens := make([]string, l)
	e := l - 1
	i := e
	s := sm.path[e].S

	for e > 0 {
		s = sm.path[e].S
		tokens[i] = string(textRunes[s:e])
		e = s
		i--
	}

	return tokens[i+1:]
}

type NullEdge struct {
	Edge
	Valid bool
}

func (ne *NullEdge) Set(e Edge) {
	ne.Edge = e
	ne.Valid = true
}

func (sm *Segmenter) BuildPath(line []rune) {
	var (
		bestEdge NullEdge
		length   int
		word     Word
	)

	length = len(line)

	if sm.path == nil {
		sm.path = make([]Edge, length+1)
	} else {
		sm.path = sm.path[:0]
		for i := 0; i < length+1; i++ {
			sm.path = append(sm.path, Edge{})
		}
	}

	if sm.pointers != nil {
		sm.pointers = sm.pointers[:0]
	}

	word.Path = sm.path

	for i, ch := range line {
		bestEdge = NullEdge{}

		switch {
		// Check Edge type should be one of this
		// Latin, Space, Dict, Unknow
		case IsLatin(ch):
			// check end of space because current is not space
			// Replace last edge with space edge type
			if word.Type == Space {
				word.AppendEdgeAt(i)
			}

			if word.Type != Latin {
				word.Start = i
				word.Type = Latin
			}

			// check end of latin because last ch
			if i == length-1 {
				bestEdge.Set(word.GetEdge())
			}

		case IsSpace(ch):
			// check end of latin because current is not latin
			// Replace last edge with latin edge type
			if word.Type == Latin {
				word.AppendEdgeAt(i)
			}

			if word.Type != Space {
				word.Start = i
				word.Type = Space
			}

			// check end of space because last ch
			if i == length-1 {
				bestEdge.Set(word.GetEdge())
			}
		default:
			// check end of latin or end of space because current is not latin or space
			if word.Type == Space || word.Type == Latin {
				word.AppendEdgeAt(i)
			}

			word.Type = Text

			sm.pointers = append(sm.pointers, DictBuilderPointer{})
			newIndex := 0
			for j, _ := range sm.pointers {
				p := sm.pointers[j]
				childNode, found := sm.dict[PrefixTreeNode{p.NodeID, p.Offset, ch}]
				if !found {
					continue
				}
				p.NodeID = childNode.ChildID
				p.IsFinal = childNode.IsFinal
				p.Offset++
				sm.pointers[newIndex] = p
				newIndex++
			}
			sm.pointers = sm.pointers[:newIndex]

			for _, pointer := range sm.pointers {
				if pointer.IsFinal {
					s := 1 + i - pointer.Offset
					source := sm.path[s]
					edge := Edge{
						S:         s,
						WordCount: source.WordCount + 1,
						UnkCount:  source.UnkCount,
					}

					if !bestEdge.Valid ||
						edge.UnkCount < bestEdge.UnkCount ||
						(edge.UnkCount == bestEdge.UnkCount &&
							edge.WordCount <= bestEdge.WordCount) {
						bestEdge.Set(edge)
					}
				}
			}
		}

		if !bestEdge.Valid {
			source := sm.path[word.Left]
			bestEdge.Set(Edge{
				S:         word.Left,
				WordCount: source.WordCount + 1,
				UnkCount:  source.UnkCount + 1,
			})
		} else {
			word.Left = i + 1
		}
		sm.path[i+1] = bestEdge.Edge
	}
}

type WordType int

const (
	Unknow WordType = iota
	Space
	Latin
	Text
)

type Word struct {
	Left  int
	Start int
	Path  []Edge
	Type  WordType
}

func (w *Word) AppendEdgeAt(i int) {
	source := w.Path[w.Start]
	w.Path[i] = Edge{
		S:         w.Start,
		WordCount: source.WordCount + 1,
		UnkCount:  source.UnkCount,
	}
	w.Type = Unknow
	w.Left = i
}

func (w *Word) GetEdge() Edge {
	source := w.Path[w.Start]
	w.Type = Unknow

	return Edge{
		S:         w.Start,
		WordCount: source.WordCount + 1,
		UnkCount:  source.UnkCount,
	}
}
