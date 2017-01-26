package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"runtime"
	"sort"
	"strings"
)

type Etype int

const (
	DICT  Etype = 1
	UNK         = 2
	INIT        = 3
	LATIN       = 4
	SPACE       = 5
)

// Edge - edge of word graph
type Edge struct {
	S         int
	EdgeType  Etype
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
	sort.Strings(wordsWithPayload)
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

func BuildPath(line []rune, dict PrefixTree) []Edge {
	length := len(line)
	path := make([]Edge, length+1)
	path[0] = Edge{S: 0, EdgeType: INIT, WordCount: 0, UnkCount: 0}

	var (
		leftBoundary int

		startLatin int
		foundLatin bool

		startSpace int
		foundSpace bool
		bestEdge   *Edge
		pointers   []DictBuilderPointer
	)
	for i, ch := range line {
		bestEdge = nil
		// Check Edge type should be one of this
		// Latin, Space, Dict, Unknow
		if IsLatin(ch) {
			// check end of space because current is not space
			// Replace last edge with space edge type
			if foundSpace {
				source := path[startSpace]
				path[i] = Edge{
					S:         startSpace,
					EdgeType:  SPACE,
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
				source := path[startLatin]
				bestEdge = &Edge{
					S:         startLatin,
					EdgeType:  LATIN,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				}
				foundLatin = false
			}

		} else if IsSpace(ch) {
			// check end of latin because current is not latin
			// Replace last edge with latin edge type
			if foundLatin {
				source := path[startLatin]
				path[i] = Edge{
					S:         startLatin,
					EdgeType:  LATIN,
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
				source := path[startSpace]
				bestEdge = &Edge{
					S:         startSpace,
					EdgeType:  SPACE,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				}
				foundSpace = false
			}
		} else {
			// check end of latin or end of space because current is not latin or space
			if foundSpace {
				source := path[startSpace]
				path[i] = Edge{
					S:         startSpace,
					EdgeType:  SPACE,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				}
				foundSpace = false
				leftBoundary = i
			}

			if foundLatin {
				source := path[startLatin]
				path[i] = Edge{
					S:         startLatin,
					EdgeType:  LATIN,
					WordCount: source.WordCount + 1,
					UnkCount:  source.UnkCount,
				}
				foundLatin = false
				leftBoundary = i
			}

			pointers = append(pointers, DictBuilderPointer{})
			newIndex := 0
			for j, _ := range pointers {
				p := pointers[j]
				childNode, found := dict[PrefixTreeNode{p.NodeID, p.Offset, ch}]
				if !found {
					continue
				}
				p.NodeID = childNode.ChildID
				p.IsFinal = childNode.IsFinal
				p.Offset++
				pointers[newIndex] = p
				newIndex++
			}
			pointers = pointers[:newIndex]

			for _, pointer := range pointers {
				if pointer.IsFinal {
					s := 1 + i - pointer.Offset
					source := path[s]
					edge := Edge{
						S:         s,
						EdgeType:  DICT,
						WordCount: source.WordCount + 1,
						UnkCount:  source.UnkCount,
					}
					if bestEdge == nil {
						bestEdge = &edge
					} else if (edge.UnkCount < bestEdge.UnkCount) ||
						((edge.UnkCount == bestEdge.UnkCount) && (edge.WordCount <= bestEdge.WordCount)) {
						bestEdge = &edge
					}
				}
			}
		}

		if bestEdge == nil {
			source := path[leftBoundary]
			bestEdge = &Edge{
				S:         leftBoundary,
				EdgeType:  UNK,
				WordCount: source.WordCount + 1,
				UnkCount:  source.UnkCount + 1,
			}
		} else {
			leftBoundary = i + 1
		}
		path[i+1] = *bestEdge
	}
	return path
}

func Segment(line string, dict PrefixTree) []string {
	textRunes := []rune(line)
	path := BuildPath(textRunes, dict)

	l := len(path)
	tokens := make([]string, l)
	e := l - 1
	i := e
	s := path[e].S

	for e > 0 {
		s = path[e].S
		tokens[i] = string(textRunes[s:e])
		e = s
		i--
	}

	return tokens[i+1:]
}

type Data struct {
	lineNo int
	line   string
}

func MapSegemnt(lineNo int, line string, dict PrefixTree, out chan Data) {
	line = strings.Join(Segment(line, dict), "|")
	out <- Data{lineNo: lineNo, line: line}
}

func CollectResult(result map[int]string, in chan Data) {
	data := <-in
	result[data.lineNo] = data.line
}

func ConcurrentVersion() {
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
	outbuf := bufio.NewWriter(os.Stdout)
	i := 0
	result := make(map[int]string)
	chData := make(chan Data, runtime.NumCPU())
	for scanner.Scan() {
		go MapSegemnt(i, scanner.Text(), dict, chData)
		i++
	}
	for j := 0; j < i; j++ {
		CollectResult(result, chData)
	}
	for j := 0; j < i; j++ {
		fmt.Fprintln(outbuf, result[j])
	}
	outbuf.Flush()

}

func SequentialVersion() {
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
	outbuf := bufio.NewWriter(os.Stdout)
	i := 0
	for scanner.Scan() {
		fmt.Fprintln(outbuf, strings.Join(Segment(scanner.Text(), dict), "|"))
		i++
	}
	outbuf.Flush()
}

func main() {
	ConcurrentVersion()
	//SequentialVersion()
}
