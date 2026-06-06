package insider

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/provider/sina"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func NewInsiderCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "insider",
		Aliases:           []string{"in"},
		Short:             "Show executive shareholding changes",
		SilenceUsage:      true,
		SilenceErrors:     true,
		Args:              cobra.ExactArgs(1),
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE:              runInsider,
	}
	return cmd
}

func runInsider(cmd *cobra.Command, args []string) error {
	key := args[0]
	secs := sina.Search(cmd.Context(), key)
	if len(secs) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "未找到证券: %s\n", key)
		return nil
	}
	sec := secs[0]
	orgID, _, err := cninfo.LookupOrgID(cmd.Context(), sec.Code)
	if err != nil {
		return fmt.Errorf("查找证券代码失败: %w", err)
	}

	// Query CNINFO announcements with keyword search
	zReq := &cninfo.QueryRequest{
		StockCode: fmt.Sprintf("%s,%s", sec.Code, orgID),
		SearchKey: "增持",
		PageNum:   1,
		PageSize:  30,
	}
	jReq := &cninfo.QueryRequest{
		StockCode: zReq.StockCode,
		SearchKey: "减持",
		PageNum:   1,
		PageSize:  30,
	}

	var (
		zResp, jResp *cninfo.QueryResponse
		err1, err2   error
		wg           sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		zResp, err1 = cninfo.QueryAnnouncements(cmd.Context(), zReq)
	}()
	go func() {
		defer wg.Done()
		jResp, err2 = cninfo.QueryAnnouncements(cmd.Context(), jReq)
	}()
	wg.Wait()
	if err1 != nil {
		return fmt.Errorf("查询高管变动公告失败: %w", err1)
	}
	if err2 != nil {
		return fmt.Errorf("查询高管变动公告失败: %w", err2)
	}
	if zResp == nil && jResp == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "未找到 %s 的高管增减持公告\n", sec.Name)
		return nil
	}
	num := len(zResp.Data) + len(jResp.Data)
	if num == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "未找到 %s 的高管增减持公告\n", sec.Name)
		return nil
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\n证券代码: %s  证券名称: %s\n", sec.ExCode, sec.Name)
	fmt.Fprintf(out, "高管增减持公告 (共 %d 条)\n\n", num)

	anns := make([]*cninfo.Announcement, 0, num)
	if zResp != nil {
		anns = append(anns, zResp.Data...)
	}
	if jResp != nil {
		anns = append(anns, jResp.Data...)
	}
	sort.Slice(anns, func(i, j int) bool {
		return anns[i].Time > anns[j].Time
	})

	headers := []string{"公告日期", "公告标题", "类型", "大小"}
	data := make([][]string, 0, num)
	styles := make([][]tablewriter.Colors, 0, num)

	for _, a := range anns {
		if a.ExistFlag != 0 || a.InvalidationFlag != 0 {
			continue
		}
		t := time.Unix(a.Time/1000, 0).Format("2006-01-02")
		typeName := a.TypeName
		if typeName == "" {
			typeName = "-"
		}
		data = append(data, []string{t, a.Title, typeName, formatSize(float64(a.AdjunctSize))})

		style := make([]tablewriter.Colors, len(headers))
		if strings.Contains(a.Title, "增持") {
			style[1] = tablewriter.Colors{tablewriter.FgRedColor, tablewriter.Bold}
		} else if strings.Contains(a.Title, "减持") {
			style[1] = tablewriter.Colors{tablewriter.FgGreenColor, tablewriter.Bold}
		}
		styles = append(styles, style)
	}

	table := tablewriter.NewWriter(out)
	table.SetHeader(headers)
	hs := make([]tablewriter.Colors, len(headers))
	for i := range headers {
		hs[i] = tablewriter.Colors{tablewriter.Bold}
	}
	table.SetHeaderColor(hs...)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetNoWhiteSpace(true)
	table.SetTablePadding("\t")
	table.SetColWidth(60)
	for i, row := range data {
		table.Rich(row, styles[i])
	}
	table.Render()
	fmt.Fprintln(out)
	return nil
}

func formatSize(bytes float64) string {
	if bytes <= 0 {
		return "-"
	}
	if bytes >= 1_048_576 {
		return fmt.Sprintf("%.1fMB", bytes/1_048_576)
	}
	if bytes >= 1_024 {
		return fmt.Sprintf("%.1fKB", bytes/1_024)
	}
	return fmt.Sprintf("%.0fB", bytes)
}
