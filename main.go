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
		log.Fatal("could not read input:", err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	wordWithPayloads := make([]string, 0)
	for scanner.Scan() {
		if line := scanner.Text(); len(line) != 0 {
			wordWithPayloads = append(wordWithPayloads, line)
		}
	}
	sort.Strings(wordWithPayloads)
	return MakePrefixTree(wordWithPayloads), nil
}

// LoadDefaultDict - loading default Thai dictionary
func LoadDefaultDict() (PrefixTree, error) {
	_, filename, _, _ := runtime.Caller(0)
	return LoadDict(path.Join(path.Dir(filename), "tdict-std.txt"))
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

// MakePrefixTree is for constructing prefix tree for word with payload list
func MakePrefixTree(wordsWithPayload []string) PrefixTree {
	tab := make(map[PrefixTreeNode]PrefixTreePointer)
	for i, wordWithPayload := range wordsWithPayload {
		word := wordWithPayload
		rowNo := 0

		runes := []rune(word)
		for j, ch := range runes {
			isFinal := ((j + 1) == len(runes))
			node := PrefixTreeNode{rowNo, j, ch}
			child, found := tab[node]

			if !found {
				tab[node] = PrefixTreePointer{i, isFinal}
				rowNo = i
			} else {
				rowNo = child.ChildID
			}
		}
	}
	return PrefixTree(tab)
}

type Data struct {
	lineNo int
	line   string
}

func main() {
	var dixPath string
	flag.StringVar(&dixPath, "dix", "", "Dictionary path")
	flag.Parse()
	dict, err := LoadDict(dixPath)
	if err != nil {
		log.Fatal(err)
	}

	b, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal("could not read input:", err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	outbuf := bufio.NewWriterSize(os.Stdout, len(b)*2+4096)
	i := 0
	var wg sync.WaitGroup
	numWorker := runtime.NumCPU()
	smw := InitSegmentWorker(dict, numWorker)
	result := make(map[int]string)
	chData := make(chan Data, numWorker)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case data := <-chData:
				result[data.lineNo] = data.line
			case <-done:
				return
			}
		}
	}()
	for scanner.Scan() {
		wg.Add(1)
		text := scanner.Text()
		sm := <-smw
		go func(sm *Segmenter, lineNo int, textRunes []rune, out chan Data) {
			defer wg.Done()
			line := strings.Join(sm.Segment(textRunes), "|")
			out <- Data{lineNo: lineNo, line: line}
			smw <- sm
		}(sm, i, []rune(text), chData)
		i++
	}
	wg.Wait()
	done <- struct{}{}
	for j := 0; j < i; j++ {
		outbuf.WriteString(result[j])
		outbuf.WriteString("\n")
	}
	outbuf.Flush()
}

func InitSegmentWorker(dict PrefixTree, numWorker int) chan *Segmenter {
	smw := make(chan *Segmenter, numWorker)
	for i := 0; i < numWorker; i++ {
		smw <- &Segmenter{
			dict: dict,
			// path:     make([]Edge, 4096),
			// pointers: make([]DictBuilderPointer, 0, 4096),
		}
	}
	return smw
}

type Segmenter struct {
	dict         PrefixTree
	path         []Edge // for edges of graph
	leftBoundary int

	startLatin int
	foundLatin bool

	startSpace    int
	foundSpace    bool
	bestEdge      Edge
	foundBestEdge bool
	pointers      []DictBuilderPointer
	length        int
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

func (sm *Segmenter) BuildPath(line []rune) {
	sm.length = len(line)
	if sm.path == nil {
		sm.path = make([]Edge, sm.length+1)
	} else {
		sm.path = sm.path[:0]
		for i := 0; i < sm.length+1; i++ {
			sm.path = append(sm.path, Edge{})
		}
	}
	sm.leftBoundary = 0
	sm.startLatin = 0
	sm.foundLatin = false
	sm.startSpace = 0
	if sm.pointers != nil {
		sm.pointers = sm.pointers[:0]
	}

	for i, ch := range line {
		sm.bestEdge.S = 0
		sm.bestEdge.WordCount = 0
		sm.bestEdge.UnkCount = 0
		sm.foundBestEdge = false

		// Check Edge type should be one of this
		// Latin, Space, Dict, Unknow
		if IsLatin(ch) {
			// check end of space because current is not space
			// Replace last edge with space edge type
			if sm.foundSpace {
				source := sm.path[sm.startSpace]
				sm.path[i] = Edge{
					S:         sm.startSpace,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				}
				sm.foundSpace = false
				sm.leftBoundary = i
			}

			if !sm.foundLatin {
				sm.startLatin = i
				sm.foundLatin = true
			}

			// check end of latin because last ch
			if i == sm.length-1 {
				source := sm.path[sm.startLatin]
				sm.bestEdge.S = sm.startLatin
				sm.bestEdge.WordCount = source.WordCount + 1
				sm.bestEdge.UnkCount = source.UnkCount
				sm.foundBestEdge = true
				sm.foundLatin = false
			}

		} else if IsSpace(ch) {
			// check end of latin because current is not latin
			// Replace last edge with latin edge type
			if sm.foundLatin {
				source := sm.path[sm.startLatin]
				sm.path[i] = Edge{
					S:         sm.startLatin,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				}
				sm.foundLatin = false
				sm.leftBoundary = i
			}

			if !sm.foundSpace {
				sm.startSpace = i
				sm.foundSpace = true
			}

			// check end of space because last ch
			if i == sm.length-1 {
				source := sm.path[sm.startSpace]
				sm.bestEdge.S = sm.startSpace
				sm.bestEdge.WordCount = source.WordCount + 1
				sm.bestEdge.UnkCount = source.UnkCount
				sm.foundBestEdge = true
				sm.foundSpace = false
			}
		} else {
			// check end of latin or end of space because current is not latin or space
			if sm.foundSpace {
				source := sm.path[sm.startSpace]
				sm.path[i] = Edge{
					S:         sm.startSpace,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				}
				sm.foundSpace = false
				sm.leftBoundary = i
			}

			if sm.foundLatin {
				source := sm.path[sm.startLatin]
				sm.path[i] = Edge{
					S:         sm.startLatin,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				}
				sm.foundLatin = false
				sm.leftBoundary = i
			}

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
					if !sm.foundBestEdge {
						sm.bestEdge = edge
						sm.foundBestEdge = true
					} else if (edge.UnkCount < sm.bestEdge.UnkCount) ||
						((edge.UnkCount == sm.bestEdge.UnkCount) && (edge.WordCount <= sm.bestEdge.WordCount)) {
						sm.bestEdge = edge
						sm.foundBestEdge = true
					}
				}
			}
		}

		if !sm.foundBestEdge {
			source := sm.path[sm.leftBoundary]
			sm.bestEdge.S = sm.leftBoundary
			sm.bestEdge.WordCount = source.WordCount + 1
			sm.bestEdge.UnkCount = source.UnkCount + 1
			sm.foundBestEdge = true
		} else {
			sm.leftBoundary = i + 1
		}
		sm.path[i+1] = sm.bestEdge
	}
}
