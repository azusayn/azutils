// only used for Postgres.
package sql

import (
	"fmt"
	"strings"
)

// return built statment & values.
// NOTES:
//  1. colNames[0] must be the search key. In most cases,
//     it has the primary key constraint.
//  2. format of the colValues:
//     colValues[0]: c0v1 c0v2 c0v3 ...
//     colValues[1]: c1v1 c1v2 c1v3 ...
//     colValues[2]: c2v1 c2v2 c2v3 ...
//  3. len(colNames) == len(colValues), len(colNames) >= 1
//  4. some values may be incorrectly inferred as text by Postgres
//     use colTypes to specify the type for each column if needed.
func BuildBatchUpdateSQL(tableName string, colNames []string, colTypes []string, colValues [][]any) (string, []any) {
	numTgtCols := len(colNames)
	numTgtRows := len(colValues[0])
	searchKey := colNames[0]

	args := make([]string, 0, numTgtRows)
	values := make([]any, 0, numTgtRows*numTgtCols)
	for i := 0; i < numTgtRows; i++ {
		base := i * numTgtCols
		placeholders := make([]string, numTgtCols)
		for j := 0; j < numTgtCols; j++ {
			value := colValues[j][i]
			placeholders[j] = fmt.Sprintf("$%d%s", base+j+1, colTypes[j])
			values = append(values, value)
		}
		args = append(args, "("+strings.Join(placeholders, ",")+")")
	}

	sets := make([]string, numTgtCols-1)
	for i, c := range colNames[1:] {
		sets[i] = fmt.Sprintf("%s=v.%s", c, c)
	}

	stmt := fmt.Sprintf(`
			UPDATE %s
			SET %s
			FROM (VALUES %s) AS v(%s)
			WHERE %s.%s = v.%s
    `,
		tableName,
		strings.Join(sets, ","),
		strings.Join(args, ","),
		strings.Join(colNames, ","),
		tableName,
		searchKey,
		searchKey,
	)
	return stmt, values
}

// row first.
func BuildBatchInsertSQL(tableName string, colNames []string, rowValues [][]any) (string, []any) {
	lenCols := len(colNames)
	lenRows := len(rowValues)

	args := make([]string, 0, lenRows)
	values := make([]any, 0, lenRows*lenCols)

	for i, row := range rowValues {
		base := i * lenCols
		placeholders := make([]string, lenCols)
		for j := 0; j < lenCols; j++ {
			placeholders[j] = fmt.Sprintf("$%d", base+j+1)
		}
		args = append(args, fmt.Sprintf("(%s)", strings.Join(placeholders, ",")))
		values = append(values, row...)
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
