package jaeger

const (
	DefaultSpansTable      TableName = "jaeger_spans"
	DefaultSpansIndexTable TableName = "jaeger_index"
	DefaultOperationsTable TableName = "jaeger_operations"
)

type TableName string

func (t TableName) ToLocal() TableName {
	return t + "_local"
}

func (t TableName) ToView() TableName {
	return t + "_view"
}

type TableArgs struct {
	Database string

	SpansIndexTable     TableName
	SpansTable          TableName
	OperationsTable     TableName
	OperationsViewTable TableName
	SpansArchiveTable   TableName

	TTLTimestamp string
	TTLDate      string

	Multitenant bool
	Replication bool
	Cluster     string
}

type DistributedTableArgs struct {
	Cluster  string
	Database string
	Table    TableName
	Hash     string
}
