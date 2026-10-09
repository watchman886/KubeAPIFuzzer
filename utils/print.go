package utils

import (
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/sirupsen/logrus"
)

func PrintStatusReport(httpStatusCodeCount map[int]int) {
	// calculate total counts
	var total int
	for _, codeCount := range httpStatusCodeCount {
		total += codeCount
	}

	// create new tablewriter Writer，print to stdout
	table := tablewriter.NewTable(os.Stdout,
		tablewriter.WithConfig(tablewriter.Config{
			Row: tw.CellConfig{
				Alignment: tw.CellAlignment{
					Global: tw.AlignLeft,
				},
			},
		}),
	)

	table.Header([]string{"Status", "Count", "Percent"})

	// sort by status code
	var codes []int
	for code := range httpStatusCodeCount {
		codes = append(codes, code)
	}
	sort.Ints(codes)

	for _, code := range codes {
		codeCount := httpStatusCodeCount[code]
		pct := float64(codeCount) / float64(total) * 100
		err := table.Append([]string{
			strconv.Itoa(code),
			strconv.Itoa(codeCount),
			fmt.Sprintf("%.2f%%", pct),
		})
		if err != nil {
			logrus.Errorf("Failed to append row to table: %v", err)
		}
	}

	err := table.Render()
	if err != nil {
		logrus.Errorf("Failed to render table: %v", err)
	}
}
