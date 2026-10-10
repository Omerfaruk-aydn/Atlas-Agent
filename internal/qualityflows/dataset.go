package qualityflows

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"regexp"
	"time"
)

var datasetName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

type DatasetField struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Prefix      string   `json:"prefix,omitempty"`
	Min         int64    `json:"min,omitempty"`
	Max         int64    `json:"max,omitempty"`
	Values      []string `json:"values,omitempty"`
	Entity      string   `json:"entity,omitempty"`
	Field       string   `json:"field,omitempty"`
	Start       string   `json:"start,omitempty"`
	StepSeconds int64    `json:"step_seconds,omitempty"`
	// NullEvery deliberately creates nullable boundary cases.
	NullEvery  int `json:"null_every,omitempty"`
	EmptyEvery int `json:"empty_every,omitempty"`
}

type DatasetEntity struct {
	Name   string         `json:"name"`
	Count  int            `json:"count"`
	Fields []DatasetField `json:"fields"`
}

type DatasetSpec struct {
	Version  int             `json:"version"`
	Seed     uint64          `json:"seed"`
	Output   string          `json:"output"`
	Entities []DatasetEntity `json:"entities"`
}

type DatasetReport struct {
	SpecHash string         `json:"spec_hash"`
	DataHash string         `json:"data_hash"`
	Seed     uint64         `json:"seed"`
	Rows     map[string]int `json:"rows"`
	Bytes    int            `json:"bytes"`
	Output   string         `json:"output"`
}

func ValidateDataset(spec DatasetSpec) error {
	if spec.Version != 1 || len(spec.Entities) < 1 || len(spec.Entities) > 32 {
		return fmt.Errorf("version 1 and 1-32 entities required")
	}
	if _, err := ProjectPath(".", spec.Output); err != nil {
		return err
	}
	known := map[string]map[string]DatasetField{}
	total := 0
	for _, entity := range spec.Entities {
		if !datasetName.MatchString(entity.Name) || known[entity.Name] != nil || entity.Count < 1 || entity.Count > 10000 || len(entity.Fields) < 1 || len(entity.Fields) > 64 {
			return fmt.Errorf("invalid dataset entity: %s", entity.Name)
		}
		total += entity.Count * len(entity.Fields)
		if total > 100000 {
			return fmt.Errorf("dataset exceeds 100000 cells")
		}
		fields := map[string]DatasetField{}
		for _, field := range entity.Fields {
			if !datasetName.MatchString(field.Name) || fields[field.Name].Name != "" || len(field.Prefix) > 256 || field.NullEvery < 0 || field.EmptyEvery < 0 {
				return fmt.Errorf("invalid dataset field: %s", field.Name)
			}
			if field.EmptyEvery > 0 && field.Kind != "string" && field.Kind != "email" && field.Kind != "enum" {
				return fmt.Errorf("empty_every requires a string kind")
			}
			switch field.Kind {
			case "id":
				if field.NullEvery > 0 || field.EmptyEvery > 0 {
					return fmt.Errorf("id must remain unique and non-null")
				}
			case "string", "email", "bool":
			case "int":
				if field.Min > field.Max || field.Min < -1000000000000 || field.Max > 1000000000000 {
					return fmt.Errorf("invalid integer range")
				}
			case "enum":
				if len(field.Values) == 0 || len(field.Values) > 128 {
					return fmt.Errorf("enum needs 1-128 values")
				}
				for _, v := range field.Values {
					if len(v) > 4096 {
						return fmt.Errorf("enum value too long")
					}
				}
			case "date":
				start, err := time.Parse(time.RFC3339, field.Start)
				if err != nil || start.Year() < 1970 || start.Year() > 2100 || field.StepSeconds < 0 || field.StepSeconds > 86400*366 || time.Unix(start.Unix()+int64(entity.Count-1)*field.StepSeconds, 0).Year() > 2100 {
					return fmt.Errorf("invalid date generation bounds")
				}
			case "reference":
				parent, ok := known[field.Entity][field.Field]
				if !ok || parent.Kind != "id" {
					return fmt.Errorf("reference requires a previously declared entity's id field")
				}
			default:
				return fmt.Errorf("unsupported generator: %s", field.Kind)
			}
			fields[field.Name] = field
		}
		known[entity.Name] = fields
	}
	return nil
}

// GenerateDataset is deterministic for the seed and complete ordered spec.
// It never imports production records or uses real email addresses.
func GenerateDataset(ctx context.Context, spec DatasetSpec) ([]byte, error) {
	if err := ValidateDataset(spec); err != nil {
		return nil, err
	}
	rng := rand.New(rand.NewPCG(spec.Seed, spec.Seed^0x9e3779b97f4a7c15))
	data := map[string][]map[string]any{}
	encodedBytes := 0
	for _, entity := range spec.Entities {
		rows := make([]map[string]any, 0, entity.Count)
		for i := 1; i <= entity.Count; i++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			row := map[string]any{}
			for _, field := range entity.Fields {
				var value any
				switch field.Kind {
				case "id":
					value = field.Prefix + fmt.Sprint(i)
				case "string":
					value = field.Prefix + fmt.Sprintf("%s_%06d_%08x", field.Name, i, rng.Uint32())
				case "email":
					value = fmt.Sprintf("fixture_%06d_%08x@example.invalid", i, rng.Uint32())
				case "int":
					value = field.Min + rng.Int64N(field.Max-field.Min+1)
				case "bool":
					value = rng.IntN(2) == 1
				case "enum":
					value = field.Values[rng.IntN(len(field.Values))]
				case "date":
					start, _ := time.Parse(time.RFC3339, field.Start)
					value = time.Unix(start.Unix()+int64(i-1)*field.StepSeconds, 0).UTC().Format(time.RFC3339)
				case "reference":
					parents := data[field.Entity]
					value = parents[rng.IntN(len(parents))][field.Field]
				}
				if field.EmptyEvery > 0 && i%field.EmptyEvery == 0 {
					value = ""
				}
				if field.NullEvery > 0 && i%field.NullEvery == 0 {
					value = nil
				}
				row[field.Name] = value
			}
			encodedRow, err := json.Marshal(row)
			if err != nil {
				return nil, err
			}
			encodedBytes += len(encodedRow)
			if encodedBytes > 4*1024*1024 {
				return nil, fmt.Errorf("generated dataset exceeds 4 MiB")
			}
			rows = append(rows, row)
		}
		data[entity.Name] = rows
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(encoded) > 4*1024*1024 {
		return nil, fmt.Errorf("generated dataset exceeds 4 MiB")
	}
	return append(encoded, '\n'), nil
}
