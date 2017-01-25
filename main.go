package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/pkg/profile"
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

func BuildPath(line string, dict PrefixTree) []Edge {
	length := utf8.RuneCountInString(line)
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
	i := 0
	for _, ch := range line {
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
		i++
	}
	return path
}

type TextRange struct {
	s int
	e int
}

func PathToRanges(path []Edge) []TextRange {
	ranges := make([]TextRange, len(path))
	j := len(ranges) - 1
	for e := len(path) - 1; e > 0; {
		s := path[e].S
		ranges[j] = TextRange{s, e}
		j--
		e = s
	}
	return ranges[j+1:]
}

func Segment(line string, dict PrefixTree) []string {
	textRunes := []rune(line)
	paths := BuildPath(line, dict)
	ranges := PathToRanges(paths)
	tokens := make([]string, len(ranges))
	for i, r := range ranges {
		tokens[i] = string(textRunes[r.s:r.e])
	}
	return tokens
}

type Data struct {
	lineNo int
	line   string
}

func MapSegemnt(lineNo int, line string, dict PrefixTree, out chan Data) {
	out <- Data{lineNo: lineNo, line: strings.Join(Segment(line, dict), "|")}
}

func CollectResult(result map[int]string, in chan Data) {
	data := <-in
	result[data.lineNo] = data.line
}

func main() {
	s := profile.Start(profile.CPUProfile, profile.ProfilePath("."))
	defer s.Stop()
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
