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
		return nil, err
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
	tab := make(PrefixTree)
	for i, wordWithPayload := range wordsWithPayload {
		word := wordWithPayload
		rowNo := 0

		runes := []rune(word)
		for j, ch := range runes {
			isFinal := ((j + 1) == len(runes))
			node := PrefixTreeNode{rowNo, j, ch}

			if child, found := tab[node]; !found {
				tab[node] = PrefixTreePointer{i, isFinal}
				rowNo = i
			} else {
				rowNo = child.ChildID
			}
		}
	}

	return tab
}

type LineInput struct {
	lineNo    int
	textRunes []rune
}

func main() {
	// p := profile.Start(profile.CPUProfile, profile.ProfilePath("."))
	// defer p.Stop()

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

	var resultMu sync.Mutex
	outbuf := bufio.NewWriter(os.Stdout)

	var wg sync.WaitGroup
	numWorker := runtime.NumCPU()
	result := make(map[int]string)
	lineInputCh := make(chan LineInput, numWorker)
	done := make(chan struct{})

	for w := 0; w < numWorker; w++ {
		go func() {
			sm := &Segmenter{
				dict: dict,
			}
			for {
				select {
				case lineInput := <-lineInputCh:
					line := strings.Join(sm.Segment(lineInput.textRunes), "|") + "\n"
					resultMu.Lock()
					result[lineInput.lineNo] = line
					resultMu.Unlock()
					wg.Done()
				case <-done:
					return
				}
			}
		}()
	}

	i := 0
	for scanner.Scan() {
		wg.Add(1)
		text := scanner.Text()
		lineInputCh <- LineInput{
			lineNo:    i,
			textRunes: []rune(text),
		}
		i++
	}

	wg.Wait()
	close(done)

	for j := 0; j < i; j++ {
		resultMu.Lock()
		outbuf.WriteString(result[j])
		resultMu.Unlock()
	}

	outbuf.Flush()
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
		leftBoundary int
		startLatin   int
		foundLatin   bool
		startSpace   int
		foundSpace   bool
		bestEdge     NullEdge
		length       int
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

	for i, ch := range line {
		bestEdge = NullEdge{}

		// Check Edge type should be one of this
		// Latin, Space, Dict, Unknow
		if IsLatin(ch) {
			// check end of space because current is not space
			// Replace last edge with space edge type
			if foundSpace {
				source := sm.path[startSpace]
				sm.path[i] = Edge{
					S:         startSpace,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				}
				foundSpace = false
				leftBoundary = i
			}

			if !foundLatin {
				startLatin = i
				foundLatin = true
			}

			// check end of latin because last ch
			if i == length-1 {
				source := sm.path[startLatin]
				bestEdge.Set(Edge{
					S:         startLatin,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				})
				foundLatin = false
			}

		} else if IsSpace(ch) {
			// check end of latin because current is not latin
			// Replace last edge with latin edge type
			if foundLatin {
				source := sm.path[startLatin]
				sm.path[i] = Edge{
					S:         startLatin,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				}
				foundLatin = false
				leftBoundary = i
			}

			if !foundSpace {
				startSpace = i
				foundSpace = true
			}

			// check end of space because last ch
			if i == length-1 {
				source := sm.path[startSpace]
				bestEdge.Set(Edge{
					S:         startSpace,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				})
				foundSpace = false
			}
		} else {
			// check end of latin or end of space because current is not latin or space
			if foundSpace {
				source := sm.path[startSpace]
				sm.path[i] = Edge{
					S:         startSpace,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				}
				foundSpace = false
				leftBoundary = i
			}

			if foundLatin {
				source := sm.path[startLatin]
				sm.path[i] = Edge{
					S:         startLatin,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				}
				foundLatin = false
				leftBoundary = i
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
					if !bestEdge.Valid {
						bestEdge.Set(edge)
					} else if (edge.UnkCount < bestEdge.UnkCount) ||
						((edge.UnkCount == bestEdge.UnkCount) && (edge.WordCount <= bestEdge.WordCount)) {
						bestEdge.Set(edge)
					}
				}
			}
		}

		if !bestEdge.Valid {
			source := sm.path[leftBoundary]
			bestEdge.Set(Edge{
				S:         leftBoundary,
				WordCount: source.WordCount + 1,
				UnkCount:  source.UnkCount + 1,
			})
		} else {
			leftBoundary = i + 1
		}
		sm.path[i+1] = bestEdge.Edge
	}
}
