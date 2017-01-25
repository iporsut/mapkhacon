package main

import "unicode/utf8"

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

// IsBetterThan - comparing this edge to another edge

/*func (edge *Edge) IsBetterThan(another *Edge) bool {
	if edge == nil {
		return false
	}

	if another == nil {
		return true
	}

	if (edge.UnkCount < another.UnkCount) || ((edge.UnkCount == another.UnkCount) && (edge.WordCount < another.WordCount)) {
		return true
	}

	return false
}*/

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

func BuildPath(lineNo int, line string, dict PrefixTree) []Edge {
	path := make([]Edge, utf8.RuneCountInString(line)+1)
	path[0] = Edge{S: 0, EdgeType: INIT, WordCount: 0, UnkCount: 0}

	var (
		leftBoundary int

		startLatin int
		endLatin   int
		foundLatin bool

		startSpace int
		endSpace   int
		foundSpace bool
		bestEdge   *Edge
		pointers   []DictBuilderPointer
	)

	for i, ch := range line {
		var bestEdge *Edge
		// Check Edge type should be one of this
		// Latin, Space, Dict, Unknow
		if IsLatin(ch) {
			// check end of space because current is not space
			// check end of latin because last ch
			if !foundLatin {
				if IsLatin(ch) {
					startLatin = i
					foundLatin = true
				}
			}
		} else if IsSpace(ch) {
			// check end of latin because current is not latin
			// check end of space because last ch
		} else {
			// check end of latin or end of space because current is not latin or space
			pointers = append(pointers, DictBuilderPointer{})
			newIndex := 0
			for i, p := range pointers {
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
					} else if !((bestEdge.UnkCount < edge.UnkCount) || ((bestEdge.UnkCount == edge.UnkCount) && (bestEdge.WordCount < edge.WordCount))) {
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
			leftBoundary = i + 1
		}
		path[i+1] = *bestEdge
	}
	return path
}
