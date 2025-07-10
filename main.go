package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"

	_ "github.com/go-sql-driver/mysql"
)

var (
	dsn = flag.String("dsn", "user:pass@tcp(127.0.0.1:3306)/testdb", "MySQL DSN")
	db  *sql.DB
)

type KV struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

func main() {
	flag.Parse()
	var err error
	db, err = sql.Open("mysql", *dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	http.HandleFunc("/read", readHandler)
	http.HandleFunc("/update", updateHandler)
	http.HandleFunc("/insert", insertHandler)
	http.HandleFunc("/scan", scanHandler)
	http.HandleFunc("/rmw", rmwHandler)

	fmt.Println("YCSB HTTP server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// GET /read?key=...
func readHandler(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	var v string
	err := db.QueryRow(`SELECT v FROM ycsb_kv WHERE k = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(KV{Key: key, Value: v})
}

// POST /update  JSON {"key":"...","value":"..."}
func updateHandler(w http.ResponseWriter, r *http.Request) {
	var kv KV
	if err := json.NewDecoder(r.Body).Decode(&kv); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if kv.Key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	_, err := db.Exec(`UPDATE ycsb_kv SET v = ? WHERE k = ?`, kv.Value, kv.Key)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /insert  JSON {"key":"...","value":"..."}
func insertHandler(w http.ResponseWriter, r *http.Request) {
	var kv KV
	if err := json.NewDecoder(r.Body).Decode(&kv); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if kv.Key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	_, err := db.Exec(`INSERT IGNORE INTO ycsb_kv (k,v) VALUES (?,?)`, kv.Key, kv.Value)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /scan?start=100&count=10
func scanHandler(w http.ResponseWriter, r *http.Request) {
	startStr := r.URL.Query().Get("start")
	countStr := r.URL.Query().Get("count")
	start, err1 := strconv.Atoi(startStr)
	count, err2 := strconv.Atoi(countStr)
	if err1 != nil || err2 != nil || count <= 0 {
		http.Error(w, "invalid params", http.StatusBadRequest)
		return
	}
	end := start + count
	rows, err := db.Query(`SELECT k,v FROM ycsb_kv WHERE k BETWEEN ? AND ? LIMIT ?`,
		fmt.Sprintf("user%08d", start), fmt.Sprintf("user%08d", end), count)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var result []KV
	for rows.Next() {
		var kv KV
		rows.Scan(&kv.Key, &kv.Value)
		result = append(result, kv)
	}
	json.NewEncoder(w).Encode(result)
}

// POST /rmw JSON {"key":"..."}
func rmwHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// 读
	var v string
	if err := db.QueryRow(`SELECT v FROM ycsb_kv WHERE k = ?`, req.Key).Scan(&v); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	// 改（随机新值）
	newVal := randomValue()
	if _, err := db.Exec(`UPDATE ycsb_kv SET v = ? WHERE k = ?`, newVal, req.Key); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func randomValue() string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 100)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
