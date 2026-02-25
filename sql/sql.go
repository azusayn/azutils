package sql

import (
	"fmt"
	"strings"
)

// return built statment & values.
// NOTES:
//  1. name of the id column must be only 'id'
//  2. format of the colValues:
//     colValues[0]: c0v1 c0v2 c0v3 ...
//     colValues[1]: c1v1 c1v2 c1v3 ...
//     colValues[2]: c2v1 c2v2 c2v3 ...
//  3. len(ids) == len(colNames[i]), len(colNames) == len(colValues)
func BuildBatchUpdateSQL(tableName string, ids []any, colNames []string, colValues [][]any) (string, []any) {
	lenIds := len(ids)
	lenColVals := len(colValues)

	// number of the columns used in the target statment.
	// len(id) + len(other columns).
	numTgtCols := 1 + lenColVals
	args := make([]string, 0, len(ids))
	values := make([]any, 0, len(ids)*numTgtCols)

	// FROM (VALUES (1, 'macbook'), ... )
	for i := 0; i < lenIds; i++ {
		base := i * numTgtCols
		placeholders := make([]string, numTgtCols)
		for j := 0; j < numTgtCols; j++ {
			placeholders[j] = fmt.Sprintf("$%d", base+j+1)
		}
		args = append(args, "("+strings.Join(placeholders, ",")+")")
		values = append(values, ids[i])
		for j := 0; j < lenColVals; j++ {
			values = append(values, colValues[j][i])
		}
	}

	// SET product_name=v.product_name, ...
	sets := make([]string, lenColVals)
	for i, c := range colNames {
		sets[i] = fmt.Sprintf("%s=v.%s", c, c)
	}

	stmt := fmt.Sprintf(`
			UPDATE %s
			SET %s
			FROM (VALUES %s) AS v(%s)
			WHERE %s.id = v.id
    `,
		tableName,
		strings.Join(sets, ","),
		strings.Join(args, ","),
		"id,"+strings.Join(colNames, ","),
		tableName,
	)
	return stmt, values
}

func BuildBatchInsertSQL(tableName string, colNames []string, colValues [][]any) (string, []any) {
	lenCols := len(colNames)
	lenRows := len(colValues[0])

	args := make([]string, 0, lenRows)
	values := make([]any, 0, lenRows*lenCols)

	for i := 0; i < lenRows; i++ {
		base := i * lenCols
		placeholders := make([]string, lenCols)
		for j := 0; j < lenCols; j++ {
			placeholders[j] = fmt.Sprintf("$%d", base+j+1)
		}
		args = append(args, fmt.Sprintf("(%s)", strings.Join(placeholders, ",")))
		for j := 0; j < lenCols; j++ {
			values = append(values, colValues[j][i])
		}
	}

	stmt := fmt.Sprintf(`
		INSERT INTO %s(%s)
		VALUES %s
	`,
		tableName,
		strings.Join(colNames, ","),
		strings.Join(args, ","),
	)
	return stmt, values
}
