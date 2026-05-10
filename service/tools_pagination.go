package service

import (
	"github.com/mereith/nav/database"
	"github.com/mereith/nav/types"
	"github.com/mereith/nav/utils"
)

func GetToolsPage(page, pageSize int, keyword string, catelog string) ([]types.Tool, int64) {
	offset := (page - 1) * pageSize
	whereClause := "WHERE 1=1"
	args := []interface{}{}

if keyword != "" {
	whereClause += " AND (name LIKE ? OR desc LIKE ? OR url LIKE ?)"
	likeKeyword := "%" + keyword + "%"
	args = append(args, likeKeyword, likeKeyword, likeKeyword)
}

	if catelog != "" {
		whereClause += " AND catelog = ?"
		args = append(args, catelog)
	}

	// Count query
	countSQL := "SELECT count(*) FROM nav_table " + whereClause
	var total int64
	err := database.DB.QueryRow(countSQL, args...).Scan(&total)
	if err != nil {
		utils.CheckErr(err)
		return nil, 0
	}

	orderClause := " ORDER BY sort ASC, id ASC"
	if catelog != "" {
		orderClause = " ORDER BY CASE WHEN catelog_sort IS NULL OR catelog_sort=0 THEN sort ELSE catelog_sort END ASC, id ASC"
	}

	// Data query
	dataSQL := "SELECT id,name,url,logo,catelog,desc,sort,catelog_sort,hide FROM nav_table " + whereClause + orderClause + " LIMIT ? OFFSET ?"
	args = append(args, pageSize, offset)

	results := make([]types.Tool, 0)
	rows, err := database.DB.Query(dataSQL, args...)
	if err != nil {
		utils.CheckErr(err)
		return nil, 0
	}
	defer rows.Close()

	for rows.Next() {
		var tool types.Tool
		var hide interface{}
		var sort interface{}
		var catelogSort interface{}
		err = rows.Scan(&tool.Id, &tool.Name, &tool.Url, &tool.Logo, &tool.Catelog, &tool.Desc, &sort, &catelogSort, &hide)
		tool.Hide = parseBoolInt(hide)
		tool.Sort = parseInt(sort)
		tool.CatelogSort = parseInt(catelogSort)
		utils.CheckErr(err)
		results = append(results, tool)
	}
	return results, total
}
