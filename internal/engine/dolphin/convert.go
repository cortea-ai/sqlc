package dolphin

import (
	"fmt"

	pcast "github.com/pingcap/parser/ast"
	"github.com/pingcap/parser/opcode"
	"github.com/pingcap/parser/types"
	driver "github.com/pingcap/tidb/types/parser_driver"

	"github.com/kyleconroy/sqlc/internal/debug"
	"github.com/kyleconroy/sqlc/internal/sql/ast"
	"github.com/kyleconroy/sqlc/internal/sql/ast/pg"
)

type cc struct {
	paramCount int
}

func (c *cc) convertAlterTableStmt(n *pcast.AlterTableStmt) ast.Node {
	alt := &ast.AlterTableStmt{
		Table: parseTableName(n.Table),
		Cmds:  &ast.List{},
	}
	for _, spec := range n.Specs {
		switch spec.Tp {
		case pcast.AlterTableAddColumns:
			for _, def := range spec.NewColumns {
				name := def.Name.String()
				alt.Cmds.Items = append(alt.Cmds.Items, &ast.AlterTableCmd{
					Name:    &name,
					Subtype: ast.AT_AddColumn,
					Def: &ast.ColumnDef{
						Colname:   def.Name.String(),
						TypeName:  &ast.TypeName{Name: types.TypeStr(def.Tp.Tp)},
						IsNotNull: isNotNull(def),
					},
				})
			}

		case pcast.AlterTableDropColumn:
			name := spec.OldColumnName.String()
			alt.Cmds.Items = append(alt.Cmds.Items, &ast.AlterTableCmd{
				Name:    &name,
				Subtype: ast.AT_DropColumn,
				// MissingOk: spec.IfExists,
			})

		case pcast.AlterTableChangeColumn:
			// 	spew.Dump("change column", spec)

		case pcast.AlterTableModifyColumn:
			// 	spew.Dump("modify column", spec)

		case pcast.AlterTableAlterColumn:
			// 	spew.Dump("alter column", spec)

		case pcast.AlterTableAddConstraint:
			// 	spew.Dump("add const", spec)

		default:
			continue
		}
	}
	return alt
}

func opToName(o opcode.Op) string {
	switch o {
	case opcode.EQ:
		return "="
	}
	return o.String()
}

func (c *cc) convertBinaryOperationExpr(n *pcast.BinaryOperationExpr) ast.Node {
	if n.Op == opcode.LogicAnd || n.Op == opcode.LogicOr {
		return &pg.BoolExpr{
			// TODO: Set op
			Args: &ast.List{
				Items: []ast.Node{
					c.convert(n.L),
					c.convert(n.R),
				},
			},
		}
	} else {
		return &pg.A_Expr{
			// TODO: Set kind
			Name: &ast.List{
				Items: []ast.Node{
					&pg.String{Str: opToName(n.Op)},
				},
			},
			Lexpr: c.convert(n.L),
			Rexpr: c.convert(n.R),
		}
	}
}

func (c *cc) convertCreateTableStmt(n *pcast.CreateTableStmt) ast.Node {
	create := &ast.CreateTableStmt{
		Name:        parseTableName(n.Table),
		IfNotExists: n.IfNotExists,
	}
	for _, def := range n.Cols {
		var vals *ast.List
		if len(def.Tp.Elems) > 0 {
			vals = &ast.List{}
			for i := range def.Tp.Elems {
				vals.Items = append(vals.Items, &ast.String{
					Str: def.Tp.Elems[i],
				})
			}
		}
		create.Cols = append(create.Cols, &ast.ColumnDef{
			Colname:   def.Name.String(),
			TypeName:  &ast.TypeName{Name: types.TypeStr(def.Tp.Tp)},
			IsNotNull: isNotNull(def),
		})
	}
	return create
}

func (c *cc) convertColumnNameExpr(n *pcast.ColumnNameExpr) *pg.ColumnRef {
	return &pg.ColumnRef{
		Fields: &ast.List{
			Items: []ast.Node{
				&pg.String{Str: n.Name.Name.String()},
			},
		},
	}
}

func (c *cc) convertColumnNames(cols []*pcast.ColumnName) *ast.List {
	list := &ast.List{Items: []ast.Node{}}
	for i := range cols {
		name := cols[i].Name.String()
		list.Items = append(list.Items, &pg.ResTarget{
			Name: &name,
		})
	}
	return list
}

func (c *cc) convertDropTableStmt(n *pcast.DropTableStmt) ast.Node {
	drop := &ast.DropTableStmt{IfExists: n.IfExists}
	for _, name := range n.Tables {
		drop.Tables = append(drop.Tables, parseTableName(name))
	}
	return drop
}

func (c *cc) convertExistsSubqueryExpr(n *pcast.ExistsSubqueryExpr) *pg.SubLink {
	sublink := &pg.SubLink{}
	if ss, ok := c.convert(n.Sel).(*pg.SelectStmt); ok {
		sublink.Subselect = ss
	}
	return sublink
}

func (c *cc) convertFieldList(n *pcast.FieldList) *ast.List {
	fields := make([]ast.Node, len(n.Fields))
	for i := range n.Fields {
		fields[i] = c.convertSelectField(n.Fields[i])
	}
	return &ast.List{Items: fields}
}

func (c *cc) convertInsertStmt(n *pcast.InsertStmt) *pg.InsertStmt {
	debug.Dump(n)

	rels := c.convertTableRefsClause(n.Table)
	if len(rels.Items) != 1 {
		panic("expected one range var")
	}
	rel := rels.Items[0]
	rangeVar, ok := rel.(*pg.RangeVar)
	if !ok {
		panic("expected range var")
	}

	return &pg.InsertStmt{
		Relation:      rangeVar,
		Cols:          c.convertColumnNames(n.Columns),
		ReturningList: &ast.List{Items: []ast.Node{}},
		SelectStmt: &pg.SelectStmt{
			FromClause:  &ast.List{Items: []ast.Node{}},
			TargetList:  &ast.List{Items: []ast.Node{}},
			ValuesLists: c.convertLists(n.Lists),
		},
	}
}

func (c *cc) convertLists(lists [][]pcast.ExprNode) *ast.List {
	list := &ast.List{Items: []ast.Node{}}
	for _, exprs := range lists {
		inner := &ast.List{Items: []ast.Node{}}
		for _, expr := range exprs {
			inner.Items = append(inner.Items, c.convert(expr))
		}
		list.Items = append(list.Items, inner)
	}
	return list
}

func (c *cc) convertParamMarkerExpr(n *driver.ParamMarkerExpr) *pg.ParamRef {
	// Parameter numbers start at one
	c.paramCount += 1
	return &pg.ParamRef{
		Number:   c.paramCount,
		Location: n.Offset,
	}
}

func (c *cc) convertSelectField(n *pcast.SelectField) *pg.ResTarget {
	var val ast.Node
	if n.WildCard != nil {
		val = c.convertWildCardField(n.WildCard)
	} else {
		val = c.convert(n.Expr)
	}
	var name *string
	if n.AsName.O != "" {
		name = &n.AsName.O
	}
	return &pg.ResTarget{
		// TODO: Populate Indirection field
		Name:     name,
		Val:      val,
		Location: n.Offset,
	}
}

func (c *cc) convertSelectStmt(n *pcast.SelectStmt) *pg.SelectStmt {
	stmt := &pg.SelectStmt{
		TargetList:  c.convertFieldList(n.Fields),
		FromClause:  c.convertTableRefsClause(n.From),
		WhereClause: c.convert(n.Where),
	}
	if n.Limit != nil {
		stmt.LimitCount = c.convert(n.Limit.Count)
		stmt.LimitOffset = c.convert(n.Limit.Offset)
	}
	return stmt
}

func (c *cc) convertSubqueryExpr(n *pcast.SubqueryExpr) ast.Node {
	return c.convert(n.Query)
}

func (c *cc) convertTableRefsClause(n *pcast.TableRefsClause) *ast.List {
	var tables []ast.Node
	visit(n, func(n pcast.Node) {
		name, ok := n.(*pcast.TableName)
		if !ok {
			return
		}
		schema := name.Schema.String()
		rel := name.Name.String()
		tables = append(tables, &pg.RangeVar{
			Schemaname: &schema,
			Relname:    &rel,
		})
	})
	return &ast.List{Items: tables}
}

func (c *cc) convertWildCardField(n *pcast.WildCardField) *pg.ColumnRef {
	items := []ast.Node{}
	if t := n.Table.String(); t != "" {
		items = append(items, &pg.String{Str: t})
	}
	items = append(items, &pg.A_Star{})

	return &pg.ColumnRef{
		Fields: &ast.List{
			Items: items,
		},
	}
}

func (c *cc) convert(node pcast.Node) ast.Node {
	switch n := node.(type) {

	case *driver.ParamMarkerExpr:
		return c.convertParamMarkerExpr(n)

	case *pcast.AlterTableStmt:
		return c.convertAlterTableStmt(n)

	case *pcast.BinaryOperationExpr:
		return c.convertBinaryOperationExpr(n)

	case *pcast.ColumnNameExpr:
		return c.convertColumnNameExpr(n)

	case *pcast.CreateTableStmt:
		return c.convertCreateTableStmt(n)

	case *pcast.DropTableStmt:
		return c.convertDropTableStmt(n)

	case *pcast.ExistsSubqueryExpr:
		return c.convertExistsSubqueryExpr(n)

	case *pcast.InsertStmt:
		return c.convertInsertStmt(n)

	case *pcast.SelectStmt:
		return c.convertSelectStmt(n)

	case *pcast.SubqueryExpr:
		return c.convertSubqueryExpr(n)

	case nil:
		return nil

	default:
		if debug.Active {
			fmt.Printf("dolphin.convert: Unknown node type %T\n", n)
		}
		return &ast.TODO{}
	}
}
