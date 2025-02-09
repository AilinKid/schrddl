package ddl

import (
	"database/sql"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"github.com/emirpasic/gods/lists/arraylist"
	"github.com/juju/errors"
	"github.com/ngaut/log"
	"github.com/twinj/uuid"
)

func (c *testCase) generateDDLOps() error {
	if err := c.generateCreateSchema(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateDropSchema(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateAddTable(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateRenameTable(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateTruncateTable(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateModifyTableComment(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateModifyTableCharsetAndCollate(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateShardRowID(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateRebaseAutoID(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateDropTable(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateCreateView(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateAddIndex(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateRenameIndex(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateDropIndex(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateAddColumn(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateModifyColumn(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateDropColumn(); err != nil {
		return errors.Trace(err)
	}
	if err := c.generateSetDefaultValue(); err != nil {
		return errors.Trace(err)
	}
	return nil
}

type DDLKind = int

const (
	ddlAddTable DDLKind = iota
	ddlAddIndex
	ddlAddColumn
	ddlCreateSchema
	ddlCreateView

	ddlDropTable
	ddlDropIndex
	ddlDropColumn
	ddlDropSchema

	ddlRenameTable
	ddlRenameIndex
	ddlTruncateTable
	ddlShardRowID
	ddlRebaseAutoID
	ddlSetDefaultValue
	ddlModifyColumn
	ddlModifyTableComment
	ddlModifyTableCharsetAndCollate

	ddlKindNil
)

var mapOfDDLKind = map[string]DDLKind{
	"create schema": ddlCreateSchema,
	"create table":  ddlAddTable,
	"add index":     ddlAddIndex,
	"add column":    ddlAddColumn,

	"drop schema": ddlDropSchema,
	"drop table":  ddlDropTable,
	"drop index":  ddlDropIndex,
	"drop column": ddlDropColumn,

	"create view": ddlCreateView,

	"rename table":                     ddlRenameTable,
	"rename index":                     ddlRenameIndex,
	"truncate table":                   ddlTruncateTable,
	"shard row ID":                     ddlShardRowID,
	"rebase auto_increment ID":         ddlRebaseAutoID,
	"set default value":                ddlSetDefaultValue,
	"modify table comment":             ddlModifyTableComment,
	"modify table charset and collate": ddlModifyTableCharsetAndCollate,

	"modify column": ddlModifyColumn,
}

var mapOfDDLKindToString = map[DDLKind]string{
	ddlCreateSchema: "create schema",
	ddlAddTable:     "create table",
	ddlAddIndex:     "add index",
	ddlAddColumn:    "add column",

	ddlDropSchema: "drop schema",
	ddlDropTable:  "drop table",
	ddlDropIndex:  "drop index",
	ddlDropColumn: "drop column",

	ddlCreateView: "create view",

	ddlRenameTable:                  "rename table",
	ddlRenameIndex:                  "rename index",
	ddlTruncateTable:                "truncate table",
	ddlShardRowID:                   "shard row ID",
	ddlRebaseAutoID:                 "rebase auto_increment ID",
	ddlSetDefaultValue:              "set default value",
	ddlModifyTableComment:           "modify table comment",
	ddlModifyTableCharsetAndCollate: "modify table charset and collate",
	ddlModifyColumn:                 "modify column",
}

// mapOfDDLKindProbability use to control every kind of ddl request execute probability.
var mapOfDDLKindProbability = map[DDLKind]float64{
	ddlAddTable:  0.15,
	ddlDropTable: 0.15,

	ddlAddIndex:  0.8,
	ddlDropIndex: 0.5,

	ddlAddColumn:    0.8,
	ddlModifyColumn: 0.5,
	ddlDropColumn:   0.5,

	ddlCreateView: 0.30,

	ddlCreateSchema:                 0.10,
	ddlDropSchema:                   0.10,
	ddlRenameTable:                  0.50,
	ddlRenameIndex:                  0.50,
	ddlTruncateTable:                0.50,
	ddlShardRowID:                   0.30,
	ddlRebaseAutoID:                 0.15,
	ddlSetDefaultValue:              0.30,
	ddlModifyTableComment:           0.30,
	ddlModifyTableCharsetAndCollate: 0.30,
}

type ddlJob struct {
	id         int
	schemaName string
	tableName  string
	k          DDLKind
	jobState   string
	tableID    string
	schemaID   string
}

type ddlJobArg unsafe.Pointer

type ddlJobTask struct {
	ddlID      int
	k          DDLKind
	tblInfo    *ddlTestTable
	schemaInfo *ddlTestSchema
	viewInfo   *ddlTestView
	sql        string
	arg        ddlJobArg
	err        error // err is an error executed by the remote TiDB.
}

func (c *testCase) updateTableInfo(task *ddlJobTask) error {
	switch task.k {
	case ddlCreateSchema:
		return c.createSchemaJob(task)
	case ddlDropSchema:
		return c.dropSchemaJob(task)
	case ddlAddTable:
		return c.addTableInfo(task)
	case ddlRenameTable:
		return c.renameTableJob(task)
	case ddlTruncateTable:
		return c.truncateTableJob(task)
	case ddlModifyTableComment:
		return c.modifyTableCommentJob(task)
	case ddlModifyTableCharsetAndCollate:
		return c.modifyTableCharsetAndCollateJob(task)
	case ddlShardRowID:
		return c.shardRowIDJob(task)
	case ddlRebaseAutoID:
		return c.rebaseAutoIDJob(task)
	case ddlDropTable:
		return c.dropTableJob(task)
	case ddlCreateView:
		return c.createViewJob(task)
	case ddlAddIndex:
		return c.addIndexJob(task)
	case ddlRenameIndex:
		return c.renameIndexJob(task)
	case ddlDropIndex:
		return c.dropIndexJob(task)
	case ddlAddColumn:
		return c.addColumnJob(task)
	case ddlModifyColumn:
		return c.modifyColumnJob(task)
	case ddlDropColumn:
		return c.dropColumnJob(task)
	case ddlSetDefaultValue:
		return c.setDefaultValueJob(task)
	}
	return fmt.Errorf("unknow ddl task , %v", *task)
}

/*
execParaDDLSQL get a batch of ddl from taskCh, and then:
1. Parallel send every kind of DDL request to TiDB
2. Wait all DDL SQLs request finish
3. Send `admin show ddl jobs` request to TiDB to confirm parallel DDL requests execute order
4. Do the same DDL change on local with the same DDL requests executed order of TiDB
5. Judge the every DDL execution result of TiDB and local. If both of local and TiDB execute result are no wrong, or both are wrong it will be ok. Otherwise, It must be something wrong.
*/
func (c *testCase) execParaDDLSQL(taskCh chan *ddlJobTask, num int) error {
	if num == 0 {
		return nil
	}
	tasks := make([]*ddlJobTask, 0, num)
	var wg sync.WaitGroup
	for i := 0; i < num; i++ {
		task := <-taskCh
		tasks = append(tasks, task)
		wg.Add(1)
		go func(task *ddlJobTask) {
			defer wg.Done()
			opStart := time.Now()
			db := c.dbs[0]
			_, err := db.Exec(task.sql)
			if !ddlIgnoreError(err) {
				log.Infof("[ddl] [instance %d] TiDB execute %s , err %v, elapsed time:%v", c.caseIndex, task.sql, err, time.Since(opStart).Seconds())
				task.err = err
			}
		}(task)
	}
	wg.Wait()
	db := c.dbs[0]
	SortTasks, err := c.getSortTask(db, tasks)
	if err != nil {
		if ddlIgnoreError(err) {
			return nil
		}
		return err
	}
	for _, task := range SortTasks {
		err := c.updateTableInfo(task)
		if task.tblInfo != nil {
			log.Infof("[ddl] [instance %d] local execute %s, err %v , table_id %s, ddlID %v", c.caseIndex, task.sql, err, task.tblInfo.id, task.ddlID)
		} else if task.schemaInfo != nil {
			log.Infof("[ddl] [instance %d] local execute %s, err %v , schema_id %s, ddlID %v", c.caseIndex, task.sql, err, task.schemaInfo.id, task.ddlID)
		} else if task.viewInfo != nil {
			log.Infof("[ddl] [instance %d] local execute %s, err %v , view_id %s, ddlID %v", c.caseIndex, task.sql, err, task.viewInfo.id, task.ddlID)
		}
		if err == nil && task.err != nil || err != nil && task.err == nil {
			if err != nil && ddlIgnoreError(err) {
				return nil
			}
			return fmt.Errorf("Error when executing SQL: %s\n, local err: %#v, remote tidb err: %#v\n%s\n", task.sql, err, task.err, task.tblInfo.debugPrintToString())
		}
	}
	return nil
}

// execSerialDDLSQL gets a job from taskCh, and then executes the job.
func (c *testCase) execSerialDDLSQL(taskCh chan *ddlJobTask) error {
	if len(taskCh) < 1 {
		return nil
	}
	task := <-taskCh
	db := c.dbs[0]
	opStart := time.Now()
	_, err := db.Exec(task.sql)
	log.Infof("[ddl] [instance %d] %s, err: %v, elapsed time:%v", c.caseIndex, task.sql, err, time.Since(opStart).Seconds())
	if err != nil {
		if ddlIgnoreError(err) {
			return nil
		}
		if task.tblInfo != nil {
			return fmt.Errorf("Error when executing SQL: %s\n remote tidb Err: %#v\n%s\n", task.sql, err, task.tblInfo.debugPrintToString())
		} else {
			return fmt.Errorf("Error when executing SQL: %s\n remote tidb Err: %#v\n", task.sql, err)
		}
	}
	err = c.updateTableInfo(task)
	if err != nil {
		if task.tblInfo != nil {
			return fmt.Errorf("Error when executing SQL: %s\n local Err: %#v\n%s\n", task.sql, err, task.tblInfo.debugPrintToString())
		} else {
			return fmt.Errorf("Error when executing SQL: %s\n local Err: %#v\n", task.sql, err)
		}
	}
	return nil
}

func (c *testCase) generateCreateSchema() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareCreateSchema, nil, ddlCreateSchema})
	return nil
}

var dbSchemaSyntax = [...]string{"DATABASE", "SCHEMA"}

func (c *testCase) prepareCreateSchema(_ interface{}, taskCh chan *ddlJobTask) error {
	charset, collate := c.pickupRandomCharsetAndCollate()
	schema := ddlTestSchema{
		name:    uuid.NewV4().String(),
		deleted: false,
		charset: charset,
		collate: collate,
	}
	sql := fmt.Sprintf("CREATE %s `%s` CHARACTER SET '%s' COLLATE '%s'", dbSchemaSyntax[rand.Intn(len(dbSchemaSyntax))], schema.name,
		charset, collate)
	task := &ddlJobTask{
		k:          ddlCreateSchema,
		sql:        sql,
		schemaInfo: &schema,
	}
	taskCh <- task
	return nil
}

func (c *testCase) createSchemaJob(task *ddlJobTask) error {
	c.schemas[task.schemaInfo.name] = task.schemaInfo
	return nil
}

func (c *testCase) generateDropSchema() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareDropSchema, nil, ddlDropSchema})
	return nil
}

func (c *testCase) prepareDropSchema(_ interface{}, taskCh chan *ddlJobTask) error {
	schema := c.pickupRandomSchema()
	if schema == nil {
		return nil
	}
	schema.setDeleted()
	sql := fmt.Sprintf("DROP %s `%s`", dbSchemaSyntax[rand.Intn(len(dbSchemaSyntax))], schema.name)
	task := &ddlJobTask{
		k:          ddlDropSchema,
		sql:        sql,
		schemaInfo: schema,
	}
	taskCh <- task
	return nil
}

func (c *testCase) dropSchemaJob(task *ddlJobTask) error {
	if c.isSchemaDeleted(task.schemaInfo) {
		return fmt.Errorf("schema %s doesn't exist", task.schemaInfo.name)
	}
	delete(c.schemas, task.schemaInfo.name)
	return nil
}

func (c *testCase) generateAddTable() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareAddTable, nil, ddlAddTable})
	return nil
}

func (c *testCase) prepareAddTable(cfg interface{}, taskCh chan *ddlJobTask) error {
	columnCount := rand.Intn(c.cfg.TablesToCreate) + 2
	tableColumns := arraylist.New()
	for i := 0; i < columnCount; i++ {
		columns := getRandDDLTestColumns()
		for _, column := range columns {
			tableColumns.Add(column)
		}
	}

	// Generate primary key with [0, 3) size
	primaryKeyFields := rand.Intn(3)
	primaryKeys := make([]int, 0)
	if primaryKeyFields > 0 {
		// Random elections column as primary key, but also check the column whether can be primary key.
		perm := rand.Perm(tableColumns.Size())[0:primaryKeyFields]
		for _, columnIndex := range perm {
			column := getColumnFromArrayList(tableColumns, columnIndex)
			if column.canBePrimary() {
				column.isPrimaryKey = true
				primaryKeys = append(primaryKeys, columnIndex)
			}
		}
		primaryKeyFields = len(primaryKeys)
	}

	charset, collate := c.pickupRandomCharsetAndCollate()

	tableInfo := ddlTestTable{
		name:         uuid.NewV4().String(),
		columns:      tableColumns,
		indexes:      make([]*ddlTestIndex, 0),
		numberOfRows: 0,
		deleted:      0,
		comment:      uuid.NewV4().String(),
		charset:      charset,
		collate:      collate,
		lock:         new(sync.RWMutex),
	}

	sql := fmt.Sprintf("CREATE TABLE `%s` (", tableInfo.name)
	for i := 0; i < tableInfo.columns.Size(); i++ {
		if i > 0 {
			sql += ", "
		}
		column := getColumnFromArrayList(tableColumns, i)
		sql += fmt.Sprintf("`%s` %s", column.name, column.getDefinition())
	}
	if primaryKeyFields > 0 {
		sql += ", PRIMARY KEY ("
		for i, columnIndex := range primaryKeys {
			if i > 0 {
				sql += ", "
			}
			column := getColumnFromArrayList(tableColumns, columnIndex)
			sql += fmt.Sprintf("`%s`", column.name)
		}
		sql += ")"
	}
	sql += fmt.Sprintf(") COMMENT '%s' CHARACTER SET '%s' COLLATE '%s'",
		tableInfo.comment, charset, collate)

	task := &ddlJobTask{
		k:       ddlAddTable,
		sql:     sql,
		tblInfo: &tableInfo,
	}
	taskCh <- task
	return nil
}

func (c *testCase) addTableInfo(task *ddlJobTask) error {
	c.tablesLock.Lock()
	defer c.tablesLock.Unlock()
	c.tables[task.tblInfo.name] = task.tblInfo
	return nil
}

func (c *testCase) generateRenameTable() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareRenameTable, nil, ddlRenameTable})
	return nil
}

var toAsSyntax = [...]string{"TO", "AS"}

func (c *testCase) prepareRenameTable(_ interface{}, taskCh chan *ddlJobTask) error {
	c.tablesLock.Lock()
	defer c.tablesLock.Unlock()
	table := c.pickupRandomTable()
	if table == nil {
		return nil
	}
	// Shadow copy the table.
	table.lock.Lock()
	defer table.lock.Unlock()
	newTbl := *table
	table.setDeleted()
	newTbl.name = uuid.NewV4().String()
	sql := fmt.Sprintf("ALTER TABLE `%s` RENAME %s `%s`", table.name,
		toAsSyntax[rand.Intn(len(toAsSyntax))], newTbl.name)
	task := &ddlJobTask{
		k:       ddlRenameTable,
		sql:     sql,
		tblInfo: table,
		arg:     ddlJobArg(&newTbl),
	}
	taskCh <- task
	return nil
}

func (c *testCase) renameTableJob(task *ddlJobTask) error {
	c.tablesLock.Lock()
	defer c.tablesLock.Unlock()
	table := task.tblInfo
	if c.isTableDeleted(table) {
		return fmt.Errorf("table %s is not exists", table.name)
	}
	delete(c.tables, table.name)
	newTbl := (*ddlTestTable)(task.arg)
	c.tables[newTbl.name] = newTbl
	return nil
}

func (c *testCase) generateTruncateTable() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareTruncateTable, nil, ddlTruncateTable})
	return nil
}

func (c *testCase) prepareTruncateTable(_ interface{}, taskCh chan *ddlJobTask) error {
	tableToTruncate := c.pickupRandomTable()
	if tableToTruncate == nil {
		return nil
	}
	sql := fmt.Sprintf("TRUNCATE TABLE `%s`", tableToTruncate.name)
	task := &ddlJobTask{
		k:       ddlTruncateTable,
		sql:     sql,
		tblInfo: tableToTruncate,
	}
	taskCh <- task
	return nil
}

func (c *testCase) truncateTableJob(task *ddlJobTask) error {
	table := task.tblInfo
	table.lock.Lock()
	defer table.lock.Unlock()
	if c.isTableDeleted(table) {
		return fmt.Errorf("table %s is not exists", task.tblInfo.name)
	}
	table.numberOfRows = 0
	for ite := table.columns.Iterator(); ite.Next(); {
		column := ite.Value().(*ddlTestColumn)
		if !column.isGenerated() {
			column.rows.Clear()
		}
	}
	return nil
}

func (c *testCase) generateModifyTableComment() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareModifyTableComment, nil, ddlModifyTableComment})
	return nil
}

func (c *testCase) prepareModifyTableComment(_ interface{}, taskCh chan *ddlJobTask) error {
	table := c.pickupRandomTable()
	if table == nil {
		return nil
	}
	newComm := uuid.NewV4().String()
	sql := fmt.Sprintf("ALTER TABLE `%s` COMMENT '%s'", table.name, newComm)
	task := &ddlJobTask{
		k:       ddlModifyTableComment,
		tblInfo: table,
		sql:     sql,
		arg:     ddlJobArg(&newComm),
	}
	taskCh <- task
	return nil
}

func (c *testCase) modifyTableCommentJob(task *ddlJobTask) error {
	table := task.tblInfo
	if c.isTableDeleted(table) {
		return fmt.Errorf("table %s is not exists", table.name)
	}
	newComm := *((*string)(task.arg))
	table.comment = newComm
	return nil
}

type ddlModifyTableCharsetAndCollateJob struct {
	newCharset string
	newCollate string
}

func (c *testCase) generateModifyTableCharsetAndCollate() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareModifyTableCharsetAndCollate,
		nil, ddlModifyTableCharsetAndCollate})
	return nil
}

func (c *testCase) prepareModifyTableCharsetAndCollate(_ interface{}, taskCh chan *ddlJobTask) error {
	table := c.pickupRandomTable()
	if table == nil {
		return nil
	}

	// Currently only support converting utf8 to utf8mb4.
	// But since tidb has bugs when converting utf8 to utf8mb4 if table has blob column.
	// See https://github.com/pingcap/tidb/pull/10477 for more detail.
	// So if table has blob column, we doesn't change its charset.
	// TODO: Remove blob column check when the tidb bug are fixed.
	hasBlob := false
	for ite := table.columns.Iterator(); ite.Next(); {
		col := ite.Value().(*ddlTestColumn)
		if col.k == KindBLOB || col.k == KindTINYBLOB || col.k == KindMEDIUMBLOB || col.k == KindLONGBLOB {
			hasBlob = true
			break
		}
	}
	if hasBlob {
		return nil
	}
	charset, collate := c.pickupRandomCharsetAndCollate()
	if table.charset != "utf8" || charset != "utf8mb4" {
		return nil
	}
	sql := fmt.Sprintf("ALTER TABLE `%s` CHARACTER SET '%s' COLLATE '%s'",
		table.name, charset, collate)
	task := &ddlJobTask{
		k:       ddlModifyTableCharsetAndCollate,
		sql:     sql,
		tblInfo: table,
		arg: ddlJobArg(&ddlModifyTableCharsetAndCollateJob{
			newCharset: charset,
			newCollate: collate,
		}),
	}
	taskCh <- task
	return nil
}

func (c *testCase) modifyTableCharsetAndCollateJob(task *ddlJobTask) error {
	table := task.tblInfo
	if c.isTableDeleted(table) {
		return fmt.Errorf("table %s is not exists", table.name)
	}
	arg := (*ddlModifyTableCharsetAndCollateJob)(task.arg)
	table.charset = arg.newCharset
	table.collate = arg.newCollate
	return nil
}

const MaxShardRowIDBits int = 7

func (c *testCase) generateShardRowID() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareShardRowID, nil, ddlShardRowID})
	return nil
}

func (c *testCase) prepareShardRowID(_ interface{}, taskCh chan *ddlJobTask) error {
	// For table has auto_increment column, cannot set shard_row_id_bits to a non-zero value.
	// Since current create table, add column, and modify column job wouldn't create
	// auto_increment column, so ignore checking whether table has an auto_increment column
	// and just execute the set shard_row_id_bits job. This needed to be changed when auto_increment
	// column is generated possibly.
	table := c.pickupRandomTable()
	if table == nil {
		return nil
	}
	// Don't make shard row bits too large.
	shardRowId := rand.Intn(MaxShardRowIDBits)
	sql := fmt.Sprintf("ALTER TABLE `%s` SHARD_ROW_ID_BITS = %d", table.name, shardRowId)
	task := &ddlJobTask{
		k:       ddlShardRowID,
		tblInfo: table,
		sql:     sql,
		arg:     ddlJobArg(&shardRowId),
	}
	taskCh <- task
	return nil
}

func (c *testCase) shardRowIDJob(task *ddlJobTask) error {
	table := task.tblInfo
	if c.isTableDeleted(table) {
		return fmt.Errorf("table %s is not exists", table.name)
	}
	shardRowId := *((*int)(task.arg))
	table.shardRowId = int64(shardRowId)
	return nil
}

func (c *testCase) generateRebaseAutoID() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareRebaseAutoID, nil, ddlRebaseAutoID})
	return nil
}

func (c *testCase) prepareRebaseAutoID(_ interface{}, taskCh chan *ddlJobTask) error {
	table := c.pickupRandomTable()
	if table == nil {
		return nil
	}
	newAutoID := table.newRandAutoID()
	if newAutoID < 0 {
		return nil
	}
	sql := fmt.Sprintf("alter table `%s` auto_increment=%d", table.name, newAutoID)
	task := &ddlJobTask{
		k:       ddlRebaseAutoID,
		sql:     sql,
		tblInfo: table,
	}
	taskCh <- task
	return nil
}

func (c *testCase) rebaseAutoIDJob(task *ddlJobTask) error {
	// The autoID might be different from what we specified in task, so instead, do
	// a simple query to fetch the AutoID.
	table := task.tblInfo
	table.lock.Lock()
	defer table.lock.Unlock()
	if c.isTableDeleted(table) {
		return fmt.Errorf("table %s is not exists", table.name)
	}
	sql := fmt.Sprintf("select auto_increment from information_schema.tables "+
		"where table_schema='test' and table_name='%s'", table.name)
	// Ignore check error, it doesn't matter.
	c.dbs[0].QueryRow(sql).Scan(&table.autoIncID)
	return nil
}

func (c *testCase) generateDropTable() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareDropTable, nil, ddlDropTable})
	return nil
}

func (c *testCase) prepareDropTable(cfg interface{}, taskCh chan *ddlJobTask) error {
	c.tablesLock.Lock()
	defer c.tablesLock.Unlock()
	tableToDrop := c.pickupRandomTable()
	if len(c.tables) <= 1 || tableToDrop == nil {
		return nil
	}
	tableToDrop.setDeleted()
	sql := fmt.Sprintf("DROP TABLE `%s`", tableToDrop.name)

	task := &ddlJobTask{
		k:       ddlDropTable,
		sql:     sql,
		tblInfo: tableToDrop,
	}
	taskCh <- task
	return nil
}

func (c *testCase) dropTableJob(task *ddlJobTask) error {
	c.tablesLock.Lock()
	defer c.tablesLock.Unlock()
	if c.isTableDeleted(task.tblInfo) {
		return fmt.Errorf("table %s is not exists", task.tblInfo.name)
	}
	delete(c.tables, task.tblInfo.name)
	return nil
}

func (c *testCase) generateCreateView() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareCreateView, nil, ddlCreateView})
	return nil
}

func (c *testCase) prepareCreateView(_ interface{}, taskCh chan *ddlJobTask) error {
	table := c.pickupRandomTable()
	if table == nil {
		return nil
	}
	columns := table.pickupRandomColumns()
	if len(columns) == 0 {
		return nil
	}
	view := &ddlTestView{
		name:    uuid.NewV4().String(),
		columns: columns,
		table:   table,
	}
	sql := fmt.Sprintf("create view `%s` as select ", view.name)
	var i = 0
	for ; i < len(columns)-1; i++ {
		sql += fmt.Sprintf("`%s`, ", columns[i].name)
	}
	sql += fmt.Sprintf("`%s` from `%s`", columns[i].name, table.name)
	task := &ddlJobTask{
		k:        ddlCreateView,
		sql:      sql,
		viewInfo: view,
	}
	taskCh <- task
	return nil
}

func (c *testCase) createViewJob(task *ddlJobTask) error {
	c.views[task.viewInfo.name] = task.viewInfo
	return nil
}

type ddlTestIndexStrategy = int

const (
	ddlTestIndexStrategyBegin ddlTestIndexStrategy = iota
	ddlTestIndexStrategySingleColumnAtBeginning
	ddlTestIndexStrategySingleColumnAtEnd
	ddlTestIndexStrategySingleColumnRandom
	ddlTestIndexStrategyMultipleColumnRandom
	ddlTestIndexStrategyEnd
)

type ddlTestAddIndexConfig struct {
	strategy ddlTestIndexStrategy
}

type ddlIndexJobArg struct {
	index *ddlTestIndex
}

func (c *testCase) generateAddIndex() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareAddIndex, nil, ddlAddIndex})
	return nil
}

func (c *testCase) prepareAddIndex(_ interface{}, taskCh chan *ddlJobTask) error {
	table := c.pickupRandomTable()
	if table == nil {
		return nil
	}
	strategy := rand.Intn(ddlTestIndexStrategyMultipleColumnRandom) + ddlTestIndexStrategySingleColumnAtBeginning
	// build index definition
	index := ddlTestIndex{
		name:      uuid.NewV4().String(),
		signature: "",
		columns:   make([]*ddlTestColumn, 0),
	}

	switch strategy {
	case ddlTestIndexStrategySingleColumnAtBeginning:
		column0 := getColumnFromArrayList(table.columns, 0)
		if !column0.canBeIndex() {
			return nil
		}
		index.columns = append(index.columns, column0)
	case ddlTestIndexStrategySingleColumnAtEnd:
		lastColumn := getColumnFromArrayList(table.columns, table.columns.Size()-1)
		if !lastColumn.canBeIndex() {
			return nil
		}
		index.columns = append(index.columns, lastColumn)
	case ddlTestIndexStrategySingleColumnRandom:
		col := getColumnFromArrayList(table.columns, rand.Intn(table.columns.Size()))
		if !col.canBeIndex() {
			return nil
		}
		index.columns = append(index.columns, col)
	case ddlTestIndexStrategyMultipleColumnRandom:
		numberOfColumns := rand.Intn(table.columns.Size()) + 1
		// Multiple columns of one index should no more than 16.
		if numberOfColumns > 10 {
			numberOfColumns = 10
		}
		perm := rand.Perm(table.columns.Size())[:numberOfColumns]
		for _, idx := range perm {
			column := getColumnFromArrayList(table.columns, idx)
			if column.canBeIndex() {
				index.columns = append(index.columns, column)
			}
		}
	}

	if len(index.columns) == 0 {
		return nil
	}

	signature := ""
	for _, col := range index.columns {
		signature += col.name + ","
	}
	index.signature = signature

	// check whether index duplicates
	for _, idx := range table.indexes {
		if idx.signature == index.signature {
			return nil
		}
	}

	// build SQL
	sql := fmt.Sprintf("ALTER TABLE `%s` ADD INDEX `%s` (", table.name, index.name)
	for i, column := range index.columns {
		if i > 0 {
			sql += ", "
		}
		sql += fmt.Sprintf("`%s`", column.name)
	}
	sql += ")"

	arg := &ddlIndexJobArg{index: &index}
	task := &ddlJobTask{
		k:       ddlAddIndex,
		sql:     sql,
		tblInfo: table,
		arg:     ddlJobArg(arg),
	}
	taskCh <- task
	return nil
}

func (c *testCase) addIndexJob(task *ddlJobTask) error {
	jobArg := (*ddlIndexJobArg)(task.arg)
	tblInfo := task.tblInfo

	if c.isTableDeleted(tblInfo) {
		return fmt.Errorf("table %s is not exists", tblInfo.name)
	}

	for _, column := range jobArg.index.columns {
		if tblInfo.isColumnDeleted(column) {
			return fmt.Errorf("local Execute add index %s on column %s error , column is deleted", jobArg.index.name, column.name)
		}
	}
	tblInfo.indexes = append(tblInfo.indexes, jobArg.index)
	for _, column := range jobArg.index.columns {
		column.indexReferences++
	}
	return nil
}

type ddlRenameIndexArg struct {
	preIndex int
	newIndex string
}

func (c *testCase) generateRenameIndex() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareRenameIndex, nil, ddlRenameIndex})
	return nil
}

func (c *testCase) prepareRenameIndex(_ interface{}, taskCh chan *ddlJobTask) error {
	table := c.pickupRandomTable()
	if table == nil || len(table.indexes) == 0 {
		return nil
	}
	loc := rand.Intn(len(table.indexes))
	index := table.indexes[loc]
	newIndex := uuid.NewV4().String()
	sql := fmt.Sprintf("ALTER TABLE `%s` RENAME INDEX `%s` to `%s`",
		table.name, index.name, newIndex)
	task := &ddlJobTask{
		k:       ddlRenameIndex,
		sql:     sql,
		tblInfo: table,
		arg:     ddlJobArg(&ddlRenameIndexArg{loc, newIndex}),
	}
	taskCh <- task
	return nil
}

func (c *testCase) renameIndexJob(task *ddlJobTask) error {
	table := task.tblInfo
	arg := (*ddlRenameIndexArg)(task.arg)
	if c.isTableDeleted(table) {
		return fmt.Errorf("table %s is not exists", table.name)
	}
	if c.isIndexDeleted(table.indexes[arg.preIndex], table) {
		return fmt.Errorf("index %s on table %s is not exists", table.indexes[arg.preIndex].name, table.name)
	}
	table.indexes[arg.preIndex].name = arg.newIndex
	return nil
}

func (c *testCase) generateDropIndex() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareDropIndex, nil, ddlDropIndex})
	return nil
}

func (c *testCase) prepareDropIndex(_ interface{}, taskCh chan *ddlJobTask) error {
	table := c.pickupRandomTable()
	if table == nil {
		return nil
	}
	if len(table.indexes) == 0 {
		return nil
	}
	indexToDropIndex := rand.Intn(len(table.indexes))
	indexToDrop := table.indexes[indexToDropIndex]
	sql := fmt.Sprintf("ALTER TABLE `%s` DROP INDEX `%s`", table.name, indexToDrop.name)

	arg := &ddlIndexJobArg{index: indexToDrop}
	task := &ddlJobTask{
		k:       ddlDropIndex,
		sql:     sql,
		tblInfo: table,
		arg:     ddlJobArg(arg),
	}
	taskCh <- task
	return nil
}

func (c *testCase) dropIndexJob(task *ddlJobTask) error {
	jobArg := (*ddlIndexJobArg)(task.arg)
	tblInfo := task.tblInfo

	if c.isTableDeleted(tblInfo) {
		return fmt.Errorf("table %s is not exists", tblInfo.name)
	}

	iOfDropIndex := -1
	for i := range tblInfo.indexes {
		if jobArg.index.name == tblInfo.indexes[i].name {
			iOfDropIndex = i
			break
		}
	}
	if iOfDropIndex == -1 {
		return fmt.Errorf("table %s , index %s is not exists", tblInfo.name, jobArg.index.name)
	}

	for _, column := range jobArg.index.columns {
		column.indexReferences--
		if column.indexReferences < 0 {
			return fmt.Errorf("drop index, index.column %s Unexpected index reference", column.name)
		}
	}
	tblInfo.indexes = append(tblInfo.indexes[:iOfDropIndex], tblInfo.indexes[iOfDropIndex+1:]...)
	return nil
}

type ddlTestAddDropColumnStrategy = int

const (
	ddlTestAddDropColumnStrategyBegin ddlTestAddDropColumnStrategy = iota
	ddlTestAddDropColumnStrategyAtBeginning
	ddlTestAddDropColumnStrategyAtEnd
	ddlTestAddDropColumnStrategyAtRandom
	ddlTestAddDropColumnStrategyEnd
)

type ddlTestAddDropColumnConfig struct {
	strategy ddlTestAddDropColumnStrategy
}

type ddlColumnJobArg struct {
	origColumnIndex   int
	origColumn        *ddlTestColumn
	column            *ddlTestColumn
	strategy          ddlTestAddDropColumnStrategy
	insertAfterColumn *ddlTestColumn
}

func (c *testCase) generateAddColumn() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareAddColumn, nil, ddlAddColumn})
	return nil
}

func (c *testCase) prepareAddColumn(_ interface{}, taskCh chan *ddlJobTask) error {
	table := c.pickupRandomTable()
	if table == nil {
		return nil
	}
	strategy := rand.Intn(ddlTestAddDropColumnStrategyAtRandom) + ddlTestAddDropColumnStrategyAtBeginning
	newColumn := getRandDDLTestColumn()
	insertAfterPosition := -1
	// build SQL
	sql := fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s", table.name, newColumn.name, newColumn.getDefinition())
	switch strategy {
	case ddlTestAddDropColumnStrategyAtBeginning:
		sql += " FIRST"
	case ddlTestAddDropColumnStrategyAtEnd:
		// do nothing
	case ddlTestAddDropColumnStrategyAtRandom:
		insertAfterPosition = rand.Intn(table.columns.Size())
		column := getColumnFromArrayList(table.columns, insertAfterPosition)
		sql += fmt.Sprintf(" AFTER `%s`", column.name)
	}

	arg := &ddlColumnJobArg{
		column:   newColumn,
		strategy: strategy,
	}
	if insertAfterPosition != -1 {
		arg.insertAfterColumn = getColumnFromArrayList(table.columns, insertAfterPosition)
	}
	task := &ddlJobTask{
		k:       ddlAddColumn,
		sql:     sql,
		tblInfo: table,
		arg:     ddlJobArg(arg),
	}
	taskCh <- task
	return nil
}

func (c *testCase) addColumnJob(task *ddlJobTask) error {
	jobArg := (*ddlColumnJobArg)(task.arg)
	table := task.tblInfo
	table.lock.Lock()
	defer table.lock.Unlock()

	if c.isTableDeleted(table) {
		return fmt.Errorf("table %s is not exists", table.name)
	}
	newColumn := jobArg.column
	strategy := jobArg.strategy

	newColumn.rows = arraylist.New()
	for i := 0; i < table.numberOfRows; i++ {
		newColumn.rows.Add(newColumn.defaultValue)
	}

	switch strategy {
	case ddlTestAddDropColumnStrategyAtBeginning:
		table.columns.Insert(0, newColumn)
	case ddlTestAddDropColumnStrategyAtEnd:
		table.columns.Add(newColumn)
	case ddlTestAddDropColumnStrategyAtRandom:
		insertAfterPosition := -1
		for i := 0; i < table.columns.Size(); i++ {
			column := getColumnFromArrayList(table.columns, i)
			if jobArg.insertAfterColumn.name == column.name {
				insertAfterPosition = i
				break
			}
		}
		if insertAfterPosition == -1 {
			return fmt.Errorf("table %s ,insert column %s after column, column %s is not exists ", table.name, newColumn.name, jobArg.insertAfterColumn.name)
		}
		table.columns.Insert(insertAfterPosition+1, newColumn)
	}
	return nil
}

func (c *testCase) generateModifyColumn() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareModifyColumn, nil, ddlModifyColumn})
	return nil
}

func (c *testCase) prepareModifyColumn(_ interface{}, taskCh chan *ddlJobTask) error {
	table := c.pickupRandomTable()
	if table == nil {
		return nil
	}
	table.lock.Lock()
	defer table.lock.Unlock()
	origColIndex, origColumn := table.pickupRandomColumn()
	if origColumn == nil || !origColumn.canBeModified() {
		return nil
	}
	var modifiedColumn *ddlTestColumn
	var sql string
	if rand.Float64() > 0.5 {
		// If a column has dependency, it cannot be renamed.
		if origColumn.hasGenerateCol() {
			return nil
		}
		modifiedColumn = generateRandModifiedColumn(origColumn, true)
		origColumn.setRenamed()
		sql = fmt.Sprintf("alter table `%s` change column `%s` `%s` %s", table.name,
			origColumn.name, modifiedColumn.name, modifiedColumn.getDefinition())
	} else {
		modifiedColumn = generateRandModifiedColumn(origColumn, false)
		sql = fmt.Sprintf("alter table `%s` modify column `%s` %s", table.name,
			origColumn.name, modifiedColumn.getDefinition())
	}
	strategy := rand.Intn(ddlTestAddDropColumnStrategyAtRandom) + ddlTestAddDropColumnStrategyAtBeginning
	var insertAfterColumn *ddlTestColumn = nil
	switch strategy {
	case ddlTestAddDropColumnStrategyAtBeginning:
		sql += " FIRST"
	case ddlTestAddDropColumnStrategyAtEnd:
		endColumn := getColumnFromArrayList(table.columns, table.columns.Size()-1)
		if endColumn.name != origColumn.name {
			sql += fmt.Sprintf(" AFTER `%s`", endColumn.name)
		}
	case ddlTestAddDropColumnStrategyAtRandom:
		insertPosition := rand.Intn(table.columns.Size())
		insertAfterColumn = getColumnFromArrayList(table.columns, insertPosition)
		if insertPosition != origColIndex {
			sql += fmt.Sprintf(" AFTER `%s`", insertAfterColumn.name)
		}
	}
	task := &ddlJobTask{
		k:       ddlModifyColumn,
		tblInfo: table,
		sql:     sql,
		arg: ddlJobArg(&ddlColumnJobArg{
			origColumnIndex:   origColIndex,
			origColumn:        origColumn,
			column:            modifiedColumn,
			strategy:          strategy,
			insertAfterColumn: insertAfterColumn,
		}),
	}
	taskCh <- task
	return nil
}

func (c *testCase) modifyColumnJob(task *ddlJobTask) error {
	table := task.tblInfo
	table.lock.Lock()
	defer table.lock.Unlock()
	if c.isTableDeleted(table) {
		return fmt.Errorf("table %s is not exists", table.name)
	}
	arg := (*ddlColumnJobArg)(task.arg)
	if c.isColumnDeleted(arg.origColumn, table) {
		return fmt.Errorf("column %s on table %s is not exists", arg.origColumn.name, table.name)
	}
	table.columns.Remove(arg.origColumnIndex)
	switch arg.strategy {
	case ddlTestAddDropColumnStrategyAtBeginning:
		table.columns.Insert(0, arg.column)
	case ddlTestAddDropColumnStrategyAtEnd:
		table.columns.Add(arg.column)
	case ddlTestAddDropColumnStrategyAtRandom:
		insertPosition := arg.origColumnIndex - 1
		for i := 0; i < table.columns.Size(); i++ {
			col := getColumnFromArrayList(table.columns, i)
			if col.name == arg.insertAfterColumn.name {
				insertPosition = i
				break
			}
		}
		table.columns.Insert(insertPosition+1, arg.column)
	}
	return nil
}

func (c *testCase) generateDropColumn() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareDropColumn, nil, ddlDropColumn})
	return nil
}

func (c *testCase) prepareDropColumn(_ interface{}, taskCh chan *ddlJobTask) error {
	table := c.pickupRandomTable()
	if table == nil {
		return nil
	}

	columnsSnapshot := table.filterColumns(table.predicateAll)
	if len(columnsSnapshot) <= 1 {
		return nil
	}

	strategy := rand.Intn(ddlTestAddDropColumnStrategyAtRandom) + ddlTestAddDropColumnStrategyAtBeginning
	columnToDropIndex := -1
	switch strategy {
	case ddlTestAddDropColumnStrategyAtBeginning:
		columnToDropIndex = 0
	case ddlTestAddDropColumnStrategyAtEnd:
		columnToDropIndex = table.columns.Size() - 1
	case ddlTestAddDropColumnStrategyAtRandom:
		columnToDropIndex = rand.Intn(table.columns.Size())
	}

	columnToDrop := getColumnFromArrayList(table.columns, columnToDropIndex)

	// Primary key columns cannot be dropped
	if columnToDrop.isPrimaryKey {
		return nil
	}

	// Column cannot be dropped if the column has generated column dependency
	if columnToDrop.hasGenerateCol() {
		return nil
	}

	// We does not support dropping a column with index
	if columnToDrop.indexReferences > 0 {
		return nil
	}
	columnToDrop.setDeleted()
	sql := fmt.Sprintf("ALTER TABLE `%s` DROP COLUMN `%s`", table.name, columnToDrop.name)

	arg := &ddlColumnJobArg{
		column:            columnToDrop,
		strategy:          strategy,
		insertAfterColumn: nil,
	}
	task := &ddlJobTask{
		k:       ddlDropColumn,
		sql:     sql,
		tblInfo: table,
		arg:     ddlJobArg(arg),
	}
	taskCh <- task
	return nil
}

func (c *testCase) dropColumnJob(task *ddlJobTask) error {
	jobArg := (*ddlColumnJobArg)(task.arg)
	table := task.tblInfo
	table.lock.Lock()
	defer table.lock.Unlock()
	if c.isTableDeleted(table) {
		return fmt.Errorf("table %s is not exists", table.name)
	}
	columnToDrop := jobArg.column
	if columnToDrop.indexReferences > 0 {
		columnToDrop.setDeletedRecover()
		return fmt.Errorf("local Execute drop column %s on table %s error , column has index reference", jobArg.column.name, table.name)
	}
	dropColumnPosition := -1
	for i := 0; i < table.columns.Size(); i++ {
		column := getColumnFromArrayList(table.columns, i)
		if columnToDrop.name == column.name {
			dropColumnPosition = i
			break
		}
	}
	if dropColumnPosition == -1 {
		return fmt.Errorf("table %s ,drop column , column %s is not exists ", table.name, columnToDrop.name)
	}
	// update table definitions
	table.columns.Remove(dropColumnPosition)
	// if the drop column is a generated column , we should update the dependency column
	if columnToDrop.isGenerated() {
		col := columnToDrop.dependency
		i := 0
		for i = range col.dependenciedCols {
			if col.dependenciedCols[i].name == columnToDrop.name {
				break
			}
		}
		col.dependenciedCols = append(col.dependenciedCols[:i], col.dependenciedCols[i+1:]...)
	}
	return nil
}

type ddlSetDefaultValueArg struct {
	columnIndex     int
	column          *ddlTestColumn
	newDefaultValue interface{}
}

func (c *testCase) generateSetDefaultValue() error {
	c.ddlOps = append(c.ddlOps, ddlTestOpExecutor{c.prepareSetDefaultValue, nil, ddlSetDefaultValue})
	return nil
}

func (c *testCase) prepareSetDefaultValue(_ interface{}, taskCh chan *ddlJobTask) error {
	table := c.pickupRandomTable()
	if table == nil {
		return nil
	}
	columns := table.filterColumns(table.predicateAll)
	if len(columns) == 0 {
		return nil
	}
	loc := rand.Intn(len(columns))
	column := columns[loc]
	// If the chosen column cannot have default value, just return nil.
	if !column.canHaveDefaultValue() {
		return nil
	}
	newDefaultValue := column.randValue()
	sql := fmt.Sprintf("ALTER TABLE `%s` ALTER `%s` SET DEFAULT %s", table.name,
		column.name, getDefaultValueString(column.k, newDefaultValue))
	task := &ddlJobTask{
		k:       ddlSetDefaultValue,
		sql:     sql,
		tblInfo: table,
		arg: ddlJobArg(&ddlSetDefaultValueArg{
			columnIndex:     loc,
			column:          column,
			newDefaultValue: newDefaultValue,
		}),
	}
	taskCh <- task
	return nil
}

func (c *testCase) setDefaultValueJob(task *ddlJobTask) error {
	table := task.tblInfo
	table.lock.Lock()
	defer table.lock.Unlock()
	if c.isTableDeleted(table) {
		return fmt.Errorf("table %s is not exists", table.name)
	}
	arg := (*ddlSetDefaultValueArg)(task.arg)
	if c.isColumnDeleted(arg.column, table) {
		return fmt.Errorf("column %s on table %s is not exists", arg.column.name, table.name)
	}
	column := getColumnFromArrayList(table.columns, arg.columnIndex)
	column.defaultValue = arg.newDefaultValue
	return nil
}

// getHistoryDDLJobs send "admin show ddl jobs" to TiDB to get ddl jobs execute order.
// Use TABLE_NAME or TABLE_ID, and JOB_TYPE to confirm which ddl job is the DDL request we send to TiDB.
// We cannot send the same DDL type to same table more than once in a batch of parallel DDL request. The reason is below:
// For example, execute SQL1: "ALTER TABLE t1 DROP COLUMN c1" , SQL2:"ALTER TABLE t1 DROP COLUMN c2", and the "admin show ddl jobs" result is:
// +--------+---------+------------+--------------+--------------+-----------+----------+-----------+-----------------------------------+--------+
// | JOB_ID | DB_NAME | TABLE_NAME | JOB_TYPE     | SCHEMA_STATE | SCHEMA_ID | TABLE_ID | ROW_COUNT | START_TIME                        | STATE  |
// +--------+---------+------------+--------------+--------------+-----------+----------+-----------+-----------------------------------+--------+
// | 47     | test    | t1         | drop column  | none         | 1         | 44       | 0         | 2018-07-13 13:13:55.57 +0800 CST  | synced |
// | 46     | test    | t1         | drop column  | none         | 1         | 44       | 0         | 2018-07-13 13:13:52.523 +0800 CST | synced |
// +--------+---------+------------+--------------+--------------+-----------+----------+-----------+-----------------------------------+--------+
// We cannot confirm which DDL execute first.
func (c *testCase) getHistoryDDLJobs(db *sql.DB, tasks []*ddlJobTask) ([]*ddlJob, error) {
	// build SQL
	sql := "admin show ddl jobs"
	// execute
	opStart := time.Now()
	rows, err := db.Query(sql)
	log.Infof("%s, elapsed time:%v", sql, time.Since(opStart).Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]*ddlJob, 0, len(tasks))
	// Read all rows.
	var actualRows [][]string
	for rows.Next() {
		cols, err1 := rows.Columns()
		if err1 != nil {
			return nil, err1
		}

		rawResult := make([][]byte, len(cols))
		result := make([]string, len(cols))
		dest := make([]interface{}, len(cols))
		for i := range rawResult {
			dest[i] = &rawResult[i]
		}

		err1 = rows.Scan(dest...)
		if err1 != nil {
			return nil, err1
		}

		for i, raw := range rawResult {
			if raw == nil {
				result[i] = "NULL"
			} else {
				val := string(raw)
				result[i] = val
			}
		}
		actualRows = append(actualRows, result)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	/*********************************
	  +--------+---------+--------------------------------------+--------------+--------------+-----------+----------+-----------+-----------------------------------+-----------+
	  | JOB_ID | DB_NAME | TABLE_NAME                           | JOB_TYPE     | SCHEMA_STATE | SCHEMA_ID | TABLE_ID | ROW_COUNT | START_TIME                        | STATE     |
	  +--------+---------+--------------------------------------+--------------+--------------+-----------+----------+-----------+-----------------------------------+-----------+
	  | 49519  | test    |                                      | add column   | none         | 49481     | 49511    | 0         | 2018-07-09 21:29:02.249 +0800 CST | cancelled |
	  | 49518  | test    |                                      | drop table   | none         | 49481     | 49511    | 0         | 2018-07-09 21:29:01.999 +0800 CST | synced    |
	  | 49517  | test    | ea5be232-50ce-43b1-8d40-33de2ae08bca | create table | public       | 49481     | 49515    | 0         | 2018-07-09 21:29:01.999 +0800 CST | synced    |
	  +--------+---------+--------------------------------------+--------------+--------------+-----------+----------+-----------+-----------------------------------+-----------+
	  *********************************/
	for _, row := range actualRows {
		if len(row) < 9 {
			return nil, fmt.Errorf("%s return error, no enough column return , return row: %s", sql, row)
		}
		id, err := strconv.Atoi(row[0])
		if err != nil {
			return nil, err
		}
		if id <= c.lastDDLID {
			continue
		}
		k, ok := mapOfDDLKind[row[3]]
		if !ok {
			continue
		}
		job := ddlJob{
			id:         id,
			schemaName: row[1],
			tableName:  row[2],
			k:          k,
			schemaID:   row[5],
			tableID:    row[6], // table id
			jobState:   row[9],
		}
		jobs = append(jobs, &job)
	}
	return jobs, nil
}

// getSortTask return the tasks sort by ddl JOB_ID
func (c *testCase) getSortTask(db *sql.DB, tasks []*ddlJobTask) ([]*ddlJobTask, error) {
	jobs, err := c.getHistoryDDLJobs(db, tasks)
	if err != nil {
		return nil, err
	}
	sortTasks := make([]*ddlJobTask, 0, len(tasks))
	for _, job := range jobs {
		for _, task := range tasks {
			if task.k == ddlAddTable && job.k == ddlAddTable && task.tblInfo.name == job.tableName {
				task.ddlID = job.id
				task.tblInfo.id = job.tableID
				sortTasks = append(sortTasks, task)
				break
			}
			if task.k == ddlCreateSchema && job.k == ddlCreateSchema && task.schemaInfo.name == job.schemaName {
				task.ddlID = job.id
				task.schemaInfo.id = job.schemaID
				sortTasks = append(sortTasks, task)
				break
			}
			if task.k == ddlCreateView && job.k == ddlCreateView && task.viewInfo.name == job.tableName {
				task.ddlID = job.id
				task.viewInfo.id = job.tableID
				sortTasks = append(sortTasks, task)
				break
			}
			if task.k != ddlAddTable && job.k == task.k {
				if task.tblInfo != nil && task.tblInfo.id == job.tableID {
					task.ddlID = job.id
					sortTasks = append(sortTasks, task)
					break
				} else if task.viewInfo != nil && task.viewInfo.id == job.tableID {
					task.ddlID = job.id
					sortTasks = append(sortTasks, task)
				} else if task.schemaInfo != nil && task.schemaInfo.id == job.schemaID {
					task.ddlID = job.id
					sortTasks = append(sortTasks, task)
				}
			}
		}
		if len(sortTasks) == len(tasks) {
			break
		}
	}

	if len(sortTasks) != len(tasks) {
		str := "admin show ddl jobs len != len(tasks)\n"
		str += "admin get job\n"
		str += fmt.Sprintf("%v\t%v\t%v\t%v\t%v\t%v\t%v\n", "Job_ID", "DB_NAME", "TABLE_NAME", "JOB_TYPE", "SCHEMA_ID", "TABLE_ID", "JOB_STATE")
		for _, job := range jobs {
			str += fmt.Sprintf("%v\t%v\t%v\t%v\t%v\t%v\t%v\n", job.id, job.schemaName, job.tableName, mapOfDDLKindToString[job.k], job.schemaID, job.tableID, job.jobState)
		}
		str += "ddl tasks\n"
		str += fmt.Sprintf("%v\t%v\t%v\t%v\t%v\t%v\n", "Job_ID", "DB_NAME", "TABLE_NAME", "JOB_TYPE", "SCHEMA_ID", "TABLE_ID")
		for _, task := range tasks {
			if task.tblInfo != nil {
				// NOTE: currently all table or view operations only happened at initDB schema, but we don't know the initdb schema id, so
				// here just print "_" as the schema id which means it is the initDB schema.
				str += fmt.Sprintf("%v\t%v\t%v\t%v\t%v\t%v\n", task.ddlID, c.initDB, task.tblInfo.name, mapOfDDLKindToString[task.k], "_", task.tblInfo.id)
			} else if task.schemaInfo != nil {
				str += fmt.Sprintf("%v\t%v\t%v\t%v\t%v\t%v\n", task.ddlID, task.schemaInfo.name, "", mapOfDDLKindToString[task.k], task.schemaInfo.id, "")
			} else if task.viewInfo != nil {
				str += fmt.Sprintf("%v\t%v\t%v\t%v\t%v\t%v\n", task.ddlID, c.initDB, task.viewInfo.name, mapOfDDLKindToString[task.k], "_", task.viewInfo.id)
			}
		}

		str += "ddl sort tasks\n"
		str += fmt.Sprintf("%v\t%v\t%v\t%v\t%v\t%v\n", "Job_ID", "DB_NAME", "TABLE_NAME", "JOB_TYPE", "SCHEMA_ID", "TABLE_ID")
		for _, task := range sortTasks {
			if task.tblInfo != nil {
				str += fmt.Sprintf("%v\t%v\t%v\t%v\t%v\t%v\n", task.ddlID, c.initDB, task.tblInfo.name, mapOfDDLKindToString[task.k], "_", task.tblInfo.id)
			} else if task.schemaInfo != nil {
				str += fmt.Sprintf("%v\t%v\t%v\t%v\t%v\t%v\n", task.ddlID, task.schemaInfo.name, "", mapOfDDLKindToString[task.k], task.schemaInfo.id, "")
			} else if task.viewInfo != nil {
				str += fmt.Sprintf("%v\t%v\t%v\t%v\t%v\t%v\n", task.ddlID, c.initDB, task.viewInfo.name, mapOfDDLKindToString[task.k], "_", task.viewInfo.id)
			}
		}
		return nil, fmt.Errorf(str)
	}

	sort.Sort(ddlJobTasks(sortTasks))
	if len(sortTasks) > 0 {
		c.lastDDLID = sortTasks[len(sortTasks)-1].ddlID
	}
	return sortTasks, nil
}

type ddlJobTasks []*ddlJobTask

func (tasks ddlJobTasks) Swap(i, j int) {
	tasks[i], tasks[j] = tasks[j], tasks[i]
}

func (tasks ddlJobTasks) Len() int {
	return len(tasks)
}

func (tasks ddlJobTasks) Less(i, j int) bool {
	return tasks[i].ddlID < tasks[j].ddlID
}
