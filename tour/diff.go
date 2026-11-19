package tour

import "strings"

// ChangedLines is the lines of updated that old does not have, as
// 1-based inclusive ranges: what the tour highlights in a file. A file
// with no old text is changed throughout.
func ChangedLines(old, updated string) [][2]int {
	before, after := lines(old), lines(updated)
	kept := common(before, after)
	var ranges [][2]int
	for index := range after {
		if kept[index] {
			continue
		}
		line := index + 1
		last := len(ranges) - 1
		if last >= 0 && ranges[last][1] == line-1 {
			ranges[last][1] = line
		} else {
			ranges = append(ranges, [2]int{line, line})
		}
	}
	return ranges
}

func lines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// common marks each line of after that is in a longest common
// subsequence of before and after. Tour files are short, so the
// quadratic table costs nothing worth a cleverer diff.
func common(before, after []string) []bool {
	length := make([][]int, len(before)+1)
	for row := range length {
		length[row] = make([]int, len(after)+1)
	}
	for row := len(before) - 1; row >= 0; row-- {
		for column := len(after) - 1; column >= 0; column-- {
			if before[row] == after[column] {
				length[row][column] = length[row+1][column+1] + 1
			} else {
				length[row][column] = max(length[row+1][column],
					length[row][column+1])
			}
		}
	}
	kept := make([]bool, len(after))
	for row, column := 0, 0; row < len(before) && column < len(after); {
		switch {
		case before[row] == after[column]:
			kept[column] = true
			row, column = row+1, column+1
		case length[row+1][column] >= length[row][column+1]:
			row++
		default:
			column++
		}
	}
	return kept
}
