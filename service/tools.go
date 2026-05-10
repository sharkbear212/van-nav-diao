package service

import (
	"database/sql"
	"net/url"
	"sync"

	"github.com/mereith/nav/database"
	"github.com/mereith/nav/logger"
	"github.com/mereith/nav/types"
	"github.com/mereith/nav/utils"
)

func ImportTools(data []types.Tool) {
	var catelogs []string

	tx, err := database.DB.Begin()
	if err != nil {
		utils.CheckErr(err)
		return
	}
	defer func() {
		// 如果事务还未提交（比如发生错误提前返回），则回滚
		// sql.Tx.Rollback() returns error if already committed/rolled back, which is fine to ignore or check
		_ = tx.Rollback()
	}()

	sql_add_tool := `
		INSERT OR REPLACE INTO nav_table (id, name, catelog, url, logo, desc, sort, catelog_sort, hide)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
		`
	stmt, err := tx.Prepare(sql_add_tool)
	if err != nil {
		utils.CheckErr(err)
		return
	}
	defer stmt.Close()

	for _, v := range data {
		if !utils.In(v.Catelog, catelogs) {
			catelogs = append(catelogs, v.Catelog)
		}
		catelogSort := v.CatelogSort
		if catelogSort <= 0 {
			catelogSort = v.Sort
		}
		_, err = stmt.Exec(v.Id, v.Name, v.Catelog, v.Url, v.Logo, v.Desc, v.Sort, catelogSort, v.Hide)
		if err != nil {
			utils.CheckErr(err)
			// Continue with other items even if one fails
			continue
		}
	}

	// 显式提交事务，释放锁，以便后续 AddCatelog 可以执行
	err = tx.Commit()
	if err != nil {
		utils.CheckErr(err)
		return
	}

	for _, catelog := range catelogs {
		var addCatelogDto types.AddCatelogDto
		addCatelogDto.Name = catelog
		AddCatelog(addCatelogDto)
	}
	// 转存所有图片,异步
	go func(data []types.Tool) {
		for _, v := range data {
			UpdateImg(v.Logo)
		}
	}(data)

}

func UpdateTool(data types.UpdateToolDto) {
	tx, err := database.DB.Begin()
	utils.CheckErr(err)
	defer func() {
		_ = tx.Rollback()
	}()

	if data.Sort <= 0 {
		data.Sort, err = nextGlobalSortTx(tx)
		utils.CheckErr(err)
	}

	var oldCatelogSort sql.NullInt64
	err = tx.QueryRow("SELECT catelog_sort FROM nav_table WHERE id = ?", data.Id).Scan(&oldCatelogSort)
	utils.CheckErr(err)

	if data.CatelogSort <= 0 {
		if oldCatelogSort.Valid && oldCatelogSort.Int64 > 0 {
			data.CatelogSort = int(oldCatelogSort.Int64)
		} else {
			data.CatelogSort, err = nextCatelogSortTx(tx, data.Catelog)
			utils.CheckErr(err)
		}
	}

	sqlUpdateTool := `
		UPDATE nav_table
		SET name = ?, url = ?, logo = ?, catelog = ?, desc = ?, sort = ?, catelog_sort = ?, hide = ?
		WHERE id = ?;
		`
	stmt, err := tx.Prepare(sqlUpdateTool)
	utils.CheckErr(err)
	defer stmt.Close()
	res, err := stmt.Exec(data.Name, data.Url, data.Logo, data.Catelog, data.Desc, data.Sort, data.CatelogSort, data.Hide, data.Id)
	utils.CheckErr(err)
	_, err = res.RowsAffected()
	utils.CheckErr(err)
	utils.CheckErr(tx.Commit())
}

func AddTool(data types.AddToolDto) (int64, error) {
	// 创建一个互斥锁来保护数据库操作
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()

	tx, err := database.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	if data.Sort <= 0 {
		data.Sort, err = nextGlobalSortTx(tx)
		if err != nil {
			return 0, err
		}
	}
	if data.CatelogSort <= 0 {
		data.CatelogSort, err = nextCatelogSortTx(tx, data.Catelog)
		if err != nil {
			return 0, err
		}
	}

	sqlAddTool := `
		INSERT INTO nav_table (name, url, logo, catelog, desc, sort, catelog_sort, hide)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);
		`
	stmt, err := tx.Prepare(sqlAddTool)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	res, err := stmt.Exec(data.Name, data.Url, data.Logo, data.Catelog, data.Desc, data.Sort, data.CatelogSort, data.Hide)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	err = tx.Commit()
	if err != nil {
		return 0, err
	}
	logger.LogInfo("新增工具: %s", data.Name)

	// 在事务完成后再异步更新图片
	if data.Logo != "" {
		// UpdateImg(data.Logo)
	}

	return id, nil
}

func GetAllTool() []types.Tool {
	sql_get_all := `
		SELECT id,name,url,logo,catelog,desc,sort,catelog_sort,hide FROM nav_table ORDER BY sort ASC, id ASC;
		`
	results := make([]types.Tool, 0)
	rows, err := database.DB.Query(sql_get_all)
	utils.CheckErr(err)
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
	defer rows.Close()
	return results
}

func GetToolLogoUrlById(id int) string {
	sql_get_tool := `
		SELECT logo FROM nav_table WHERE id=?;
		`
	rows, err := database.DB.Query(sql_get_tool, id)
	utils.CheckErr(err)
	var tool types.Tool
	for rows.Next() {
		err = rows.Scan(&tool.Logo)
		utils.CheckErr(err)

	}
	defer rows.Close()
	return tool.Logo
}

func UpdateToolIcon(id int64, logo string) {
	sql_update_tool := `
		UPDATE nav_table SET logo=? WHERE id=?;
		`
	_, err := database.DB.Exec(sql_update_tool, logo, id)
	utils.CheckErr(err)
	UpdateImg(logo)
}
func UpdateToolsSort(updates []types.UpdateToolsSortDto, catelog string) error {
	tx, err := database.DB.Begin()
	if err != nil {
		return err
	}

	query := `UPDATE nav_table SET sort = ? WHERE id = ?`
	if catelog != "" {
		query = `UPDATE nav_table SET catelog_sort = ? WHERE id = ?`
	}

	stmt, err := tx.Prepare(query)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, update := range updates {
		value := update.Sort
		if catelog != "" {
			if update.CatelogSort > 0 {
				value = update.CatelogSort
			}
		}
		_, err = stmt.Exec(value, update.Id)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func DeleteTool(id int) error {
	logo := GetToolLogoUrlById(id)
	tx, err := database.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.Exec("DELETE FROM nav_table WHERE id = ?", id); err != nil {
		return err
	}
	if logo != "" {
		_, _ = tx.Exec("DELETE FROM nav_img WHERE url = ?", url.QueryEscape(logo))
	}
	if err = reindexToolsTx(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func BatchDeleteTools(ids []int) error {
	logos := make([]string, 0, len(ids))
	for _, id := range ids {
		logo := GetToolLogoUrlById(id)
		if logo != "" {
			logos = append(logos, logo)
		}
	}
	tx, err := database.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare("DELETE FROM nav_table WHERE id = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, id := range ids {
		if _, err = stmt.Exec(id); err != nil {
			return err
		}
	}
	for _, logo := range logos {
		_, _ = tx.Exec("DELETE FROM nav_img WHERE url = ?", url.QueryEscape(logo))
	}
	if err = reindexToolsTx(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func nextGlobalSortTx(tx *sql.Tx) (int, error) {
	var maxSort sql.NullInt64
	err := tx.QueryRow("SELECT MAX(sort) FROM nav_table").Scan(&maxSort)
	if err != nil {
		return 0, err
	}
	if !maxSort.Valid {
		return 1, nil
	}
	return int(maxSort.Int64) + 1, nil
}

func nextCatelogSortTx(tx *sql.Tx, catelog string) (int, error) {
	var maxSort sql.NullInt64
	err := tx.QueryRow(`
		SELECT MAX(CASE WHEN catelog_sort IS NULL OR catelog_sort = 0 THEN sort ELSE catelog_sort END)
		FROM nav_table WHERE catelog = ?
	`, catelog).Scan(&maxSort)
	if err != nil {
		return 0, err
	}
	if !maxSort.Valid {
		return 1, nil
	}
	return int(maxSort.Int64) + 1, nil
}

func reindexToolsTx(tx *sql.Tx) error {
	rows, err := tx.Query("SELECT id FROM nav_table ORDER BY sort ASC, id ASC")
	if err != nil {
		return err
	}
	defer rows.Close()
	idx := 1
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE nav_table SET sort = ? WHERE id = ?", idx, id); err != nil {
			return err
		}
		idx++
	}
	catRows, err := tx.Query("SELECT DISTINCT catelog FROM nav_table")
	if err != nil {
		return err
	}
	defer catRows.Close()
	for catRows.Next() {
		var catelog string
		if err = catRows.Scan(&catelog); err != nil {
			return err
		}
		toolRows, err := tx.Query(`
			SELECT id FROM nav_table
			WHERE catelog = ?
			ORDER BY CASE WHEN catelog_sort IS NULL OR catelog_sort = 0 THEN sort ELSE catelog_sort END ASC, id ASC
		`, catelog)
		if err != nil {
			return err
		}
		catIdx := 1
		for toolRows.Next() {
			var id int
			if err = toolRows.Scan(&id); err != nil {
				toolRows.Close()
				return err
			}
			if _, err = tx.Exec("UPDATE nav_table SET catelog_sort = ? WHERE id = ?", catIdx, id); err != nil {
				toolRows.Close()
				return err
			}
			catIdx++
		}
		toolRows.Close()
	}
	return nil
}

func parseInt(v interface{}) int {
	if v == nil {
		return 0
	}
	return int(v.(int64))
}

func parseBoolInt(v interface{}) bool {
	if v == nil {
		return false
	}
	return v.(int64) != 0
}
