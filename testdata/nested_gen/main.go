package main

import (
	"log"
	"os"

	goavro "github.com/linkedin/goavro/v2"
	parquet "github.com/parquet-go/parquet-go"
)

const avroSchema = `{
	"type": "record",
	"name": "UserNested",
	"fields": [
		{"name": "id", "type": "int"},
		{"name": "name", "type": "string"},
		{"name": "address", "type": {
			"type": "record",
			"name": "Address",
			"fields": [
				{"name": "street", "type": "string"},
				{"name": "city", "type": "string"},
				{"name": "zip", "type": "string"}
			]
		}},
		{"name": "tags", "type": {"type": "array", "items": "string"}},
		{"name": "orders", "type": {"type": "array", "items": {
			"type": "record",
			"name": "Order",
			"fields": [
				{"name": "order_id", "type": "int"},
				{"name": "amount", "type": "double"},
				{"name": "status", "type": "string"}
			]
		}}},
		{"name": "profile", "type": {
			"type": "record",
			"name": "Profile",
			"fields": [
				{"name": "stats", "type": {
					"type": "record",
					"name": "Stats",
					"fields": [
						{"name": "logins", "type": "int"},
						{"name": "score", "type": "double"}
					]
				}},
				{"name": "history", "type": {"type": "array", "items": {
					"type": "record",
					"name": "HistoryEntry",
					"fields": [
						{"name": "date", "type": "string"},
						{"name": "events", "type": {"type": "array", "items": "string"}}
					]
				}}}
			]
		}}
	]
}`

type Stats struct {
	Logins int32   `parquet:"logins"`
	Score  float64 `parquet:"score"`
}

type HistoryEntry struct {
	Date   string   `parquet:"date"`
	Events []string `parquet:"events"`
}

type Profile struct {
	Stats   Stats          `parquet:"stats"`
	History []HistoryEntry `parquet:"history"`
}

type Address struct {
	Street string `parquet:"street"`
	City   string `parquet:"city"`
	Zip    string `parquet:"zip"`
}

type Order struct {
	OrderId int32   `parquet:"order_id"`
	Amount  float64 `parquet:"amount"`
	Status  string  `parquet:"status"`
}

type UserNested struct {
	Id      int32    `parquet:"id"`
	Name    string   `parquet:"name"`
	Address Address  `parquet:"address"`
	Tags    []string `parquet:"tags"`
	Orders  []Order  `parquet:"orders"`
	Profile Profile  `parquet:"profile"`
}

func main() {
	generateAvro()
	generateParquet()
}

func generateAvro() {
	codec, err := goavro.NewCodec(avroSchema)
	if err != nil {
		log.Fatal("avro codec:", err)
	}

	f, err := os.Create("testdata/nested.avro")
	if err != nil {
		log.Fatal("create avro:", err)
	}
	defer f.Close()

	w, err := goavro.NewOCFWriter(goavro.OCFConfig{
		W:     f,
		Codec: codec,
	})
	if err != nil {
		log.Fatal("avro writer:", err)
	}

	records := []interface{}{
		map[string]interface{}{
			"id":   int32(1),
			"name": "Alice",
			"address": map[string]interface{}{
				"street": "123 Main St",
				"city":   "New York",
				"zip":    "10001",
			},
			"tags": []interface{}{"admin", "user"},
			"orders": []interface{}{
				map[string]interface{}{"order_id": int32(101), "amount": 59.99, "status": "shipped"},
				map[string]interface{}{"order_id": int32(102), "amount": 129.00, "status": "pending"},
			},
			"profile": map[string]interface{}{
				"stats": map[string]interface{}{"logins": int32(42), "score": 9.5},
				"history": []interface{}{
					map[string]interface{}{"date": "2024-01-10", "events": []interface{}{"login", "purchase", "logout"}},
					map[string]interface{}{"date": "2024-01-11", "events": []interface{}{}},
				},
			},
		},
		map[string]interface{}{
			"id":   int32(2),
			"name": "Bob",
			"address": map[string]interface{}{
				"street": "456 Oak Ave",
				"city":   "Los Angeles",
				"zip":    "90001",
			},
			"tags": []interface{}{"user"},
			"orders": []interface{}{
				map[string]interface{}{"order_id": int32(201), "amount": 39.99, "status": "delivered"},
			},
			"profile": map[string]interface{}{
				"stats": map[string]interface{}{"logins": int32(7), "score": 6.2},
				"history": []interface{}{
					map[string]interface{}{"date": "2024-01-05", "events": []interface{}{"login"}},
				},
			},
		},
		map[string]interface{}{
			"id":   int32(3),
			"name": "Charlie",
			"address": map[string]interface{}{
				"street": "789 Pine Rd",
				"city":   "Chicago",
				"zip":    "60601",
			},
			"tags":   []interface{}{"moderator", "user", "beta"},
			"orders": []interface{}{},
			"profile": map[string]interface{}{
				"stats":   map[string]interface{}{"logins": int32(0), "score": 0.0},
				"history": []interface{}{},
			},
		},
	}

	if err := w.Append(records); err != nil {
		log.Fatal("avro append:", err)
	}
}

func generateParquet() {
	f, err := os.Create("testdata/nested.parquet")
	if err != nil {
		log.Fatal("create parquet:", err)
	}
	defer f.Close()

	w := parquet.NewWriter(f)

	users := []UserNested{
		{
			Id:      1,
			Name:    "Alice",
			Address: Address{Street: "123 Main St", City: "New York", Zip: "10001"},
			Tags:    []string{"admin", "user"},
			Orders: []Order{
				{OrderId: 101, Amount: 59.99, Status: "shipped"},
				{OrderId: 102, Amount: 129.00, Status: "pending"},
			},
			Profile: Profile{
				Stats: Stats{Logins: 42, Score: 9.5},
				History: []HistoryEntry{
					{Date: "2024-01-10", Events: []string{"login", "purchase", "logout"}},
					{Date: "2024-01-11", Events: []string{}},
				},
			},
		},
		{
			Id:      2,
			Name:    "Bob",
			Address: Address{Street: "456 Oak Ave", City: "Los Angeles", Zip: "90001"},
			Tags:    []string{"user"},
			Orders: []Order{
				{OrderId: 201, Amount: 39.99, Status: "delivered"},
			},
			Profile: Profile{
				Stats: Stats{Logins: 7, Score: 6.2},
				History: []HistoryEntry{
					{Date: "2024-01-05", Events: []string{"login"}},
				},
			},
		},
		{
			Id:      3,
			Name:    "Charlie",
			Address: Address{Street: "789 Pine Rd", City: "Chicago", Zip: "60601"},
			Tags:    []string{"moderator", "user", "beta"},
			Orders:  []Order{},
			Profile: Profile{
				Stats:   Stats{Logins: 0, Score: 0},
				History: []HistoryEntry{},
			},
		},
	}

	for _, u := range users {
		if err := w.Write(u); err != nil {
			log.Fatal("parquet write:", err)
		}
	}

	if err := w.Close(); err != nil {
		log.Fatal("parquet close:", err)
	}
}
