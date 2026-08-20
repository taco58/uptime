package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

type Result struct {
	URL     string
	Status  int
	Latency time.Duration
	Success bool
}

type Website struct {
	ID     int    `json:"id"`
	URL    string `json:"url"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

var db *sql.DB

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func checkSite(url string) Result {
	start := time.Now()

	client := http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(url)

	duration := time.Since(start)

	if err != nil {
		return Result{
			URL:     url,
			Latency: duration,
			Success: false,
		}
	}

	return Result{
		URL:     url,
		Status:  resp.StatusCode,
		Latency: duration,
		Success: resp.StatusCode >= 200 && resp.StatusCode <= 399,
	}

}

func handleGetWebsites(w http.ResponseWriter, r *http.Request) {

	rows, err := db.Query("SELECT id, name, url, status FROM monitors")
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list = []Website{}
	for rows.Next() {
		var site Website
		if err := rows.Scan(&site.ID, &site.Name, &site.URL, &site.Status); err != nil {
			http.Error(w, "Error scanning row", http.StatusInternalServerError)
			return
		}
		list = append(list, site)
	}

	if rows.Err() != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func handleCreateWebsite(w http.ResponseWriter, r *http.Request) {
	var newSite Website
	err := json.NewDecoder(r.Body).Decode(&newSite)
	if err != nil {
		http.Error(w, "Invalid body request", http.StatusBadRequest)
		return
	}

	err = db.QueryRow("INSERT INTO monitors (name, url, status) VALUES ($1, $2, $3) RETURNING id",
		newSite.Name, newSite.URL, "unknown").Scan(&newSite.ID)

	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newSite)
}

func handleGetWebsiteByID(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)

	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var site Website
	err = db.QueryRow("SELECT id, name, url, status FROM monitors where id = $1", id).Scan(&site.ID, &site.Name, &site.URL, &site.Status)

	if err == sql.ErrNoRows {
		http.Error(w, "Website not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(site)
}

func handleDeleteWebsite(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)

	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	result, err := db.Exec("DELETE FROM monitors WHERE id = $1", id)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	count, err := result.RowsAffected()
	if count == 0 {
		http.Error(w, "Website not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func checkAllMonitors() {
	rows, err := db.Query("SELECT id, url FROM monitors")

	if err != nil {
		fmt.Println(err)
		return
	}
	defer rows.Close()

	var wg sync.WaitGroup

	for rows.Next() {
		var site Website
		if err := rows.Scan(&site.ID, &site.URL); err != nil {
			fmt.Println(err)
			return
		}
		wg.Add(1)

		go func(s Website) {
			defer wg.Done()

			res := checkSite(s.URL)

			db.Exec("INSERT INTO checks (monitor_id, status_code, latency_ms, success) VALUES ($1, $2, $3, $4)", s.ID, res.Status, res.Latency.Milliseconds(), res.Success)

			status := "down"

			if res.Success {
				status = "up"
			}

			db.Exec("UPDATE monitors SET status = $1 WHERE id = $2", status, s.ID)
		}(site)
	}

	wg.Wait()

	if rows.Err() != nil {
		fmt.Println(err)
		return
	}
}

func startBackgroundChecker() {
	ticker := time.NewTicker(10 * time.Second)

	for range ticker.C {
		checkAllMonitors()
	}
}

func main() {
	connStr := "postgres://localhost:5432/uptime?sslmode=disable"
	mux := http.DefaultServeMux

	var err error
	db, err = sql.Open("postgres", connStr)

	if err != nil {
		log.Fatal(err)
	}

	err = db.Ping()
	if err != nil {
		log.Fatal("Could not connect to database:", err)
	}
	fmt.Println("Connected to PostgreSQL successfully!")

	http.HandleFunc("GET /websites", handleGetWebsites)
	http.HandleFunc("GET /websites/{id}", handleGetWebsiteByID)
	http.HandleFunc("POST /websites", handleCreateWebsite)
	http.HandleFunc("DELETE /websites/{id}", handleDeleteWebsite)

	go startBackgroundChecker()

	http.ListenAndServe(":8080", enableCORS(mux))
}
