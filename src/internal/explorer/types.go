package explorer

import "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"

const (
	DefaultLimit          = 50
	MaxLimit              = 200
	MaxOffset       int64 = 1_000_000
	MaxCellBytes          = 4096
	MaxPayloadBytes       = 2 << 20
	MaxCatalogItems       = 10000
	MaxColumns            = 2000
)

type Request struct {
	Principal  security.Principal
	DatabaseID security.ID
}

type DatabaseSummary struct {
	ID     security.ID             `json:"id"`
	Name   string                  `json:"name"`
	Access security.DatabaseAccess `json:"access"`
}

type TableKind string

const (
	KindTable            TableKind = "table"
	KindPartitionedTable TableKind = "partitioned_table"
	KindView             TableKind = "view"
	KindMaterializedView TableKind = "materialized_view"
)

type TableSummary struct {
	Name string    `json:"name"`
	Kind TableKind `json:"kind"`
}

type Schema struct {
	Name   string         `json:"name"`
	Tables []TableSummary `json:"tables"`
}

type Column struct {
	Name     string  `json:"name"`
	DataType string  `json:"data_type"`
	Nullable bool    `json:"nullable"`
	Default  *string `json:"default"`
}

type Constraint struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Definition string `json:"definition"`
}

type Index struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	Unique     bool   `json:"unique"`
	Primary    bool   `json:"primary"`
}

type Table struct {
	Schema      string       `json:"schema"`
	Name        string       `json:"name"`
	Kind        TableKind    `json:"kind"`
	Columns     []Column     `json:"columns"`
	Constraints []Constraint `json:"constraints"`
	Indexes     []Index      `json:"indexes"`
}

type CellEncoding string

const (
	EncodingText CellEncoding = "text"
	EncodingHex  CellEncoding = "hex"
)

type Cell struct {
	Text      string       `json:"text"`
	Null      bool         `json:"null"`
	Truncated bool         `json:"truncated"`
	Encoding  CellEncoding `json:"encoding,omitempty"`
}

type RowOrder string

const (
	OrderPrimaryKey RowOrder = "primary_key"
	OrderBestEffort RowOrder = "best_effort"
)

type RowsQuery struct {
	Schema string
	Table  string
	Page   int
	Limit  int
}

type RowReadOptions struct {
	Offset          int64
	FetchLimit      int
	MaxCellBytes    int
	MaxPayloadBytes int
}

type RowBatch struct {
	Columns []string
	Rows    [][]Cell
	Order   RowOrder
}

type RowsPage struct {
	Columns []string `json:"columns"`
	Rows    [][]Cell `json:"rows"`
	Page    int      `json:"page"`
	Limit   int      `json:"limit"`
	HasNext bool     `json:"has_next"`
	Order   RowOrder `json:"order"`
}
