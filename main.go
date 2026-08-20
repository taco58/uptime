package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
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

type User struct {
	ID       int    `json:"id"`
	Email    string `json:"email"`
	Password string `json:"-"`
}

type AuthReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
	Token string `json:"token"`
}

var db *sql.DB
var jwtSecret []byte

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

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
	userID := r.Context().Value("user_id").(int)

	rows, err := db.Query("SELECT id, name, url, status FROM monitors WHERE user_id = $1", userID)
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
	userID := r.Context().Value("user_id").(int)

	err := json.NewDecoder(r.Body).Decode(&newSite)
	if err != nil {
		http.Error(w, "Invalid body request", http.StatusBadRequest)
		return
	}

	err = db.QueryRow("INSERT INTO monitors (name, url, status, user_id) VALUES ($1, $2, $3, $4) RETURNING id",
		newSite.Name, newSite.URL, "unknown", userID).Scan(&newSite.ID)

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
	userID := r.Context().Value("user_id").(int)

	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var site Website
	err = db.QueryRow("SELECT id, name, url, status FROM monitors where id = $1 AND user_id = $2", id, userID).Scan(&site.ID, &site.Name, &site.URL, &site.Status)

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
	userID := r.Context().Value("user_id").(int)

	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	result, err := db.Exec("DELETE FROM monitors WHERE id = $1 AND user_id = $2", id, userID)
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

func handleRegister(w http.ResponseWriter, r *http.Request) {
	var req AuthReq

	err := json.NewDecoder(r.Body).Decode(&req)

	if err != nil {
		http.Error(w, "Invalid body request", http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Password == "" {
		http.Error(w, "Invalid body request", http.StatusBadRequest)
		return
	}

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}
	passwordHash := string(hashedBytes)

	var userID int

	err = db.QueryRow("INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id", req.Email, passwordHash).Scan(&userID)

	if err != nil {
		http.Error(w, "Email already registered", http.StatusBadRequest)
		return
	}

	token, err := createToken(userID)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(AuthResponse{ID: userID, Email: req.Email, Token: token})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req AuthReq

	err := json.NewDecoder(r.Body).Decode(&req)

	if err != nil {
		http.Error(w, "Invalid body request", http.StatusBadRequest)
		return
	}

	var userID int
	var hashedPw string

	err = db.QueryRow("SELECT id, password_hash FROM users WHERE email = $1", req.Email).Scan(&userID, &hashedPw)

	if err == sql.ErrNoRows {
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(hashedPw), []byte(req.Password))
	if err != nil {
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	token, err := createToken(userID)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(AuthResponse{ID: userID, Email: req.Email, Token: token})
}

func createToken(userID int) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(24 * time.Hour).Unix(), // expires in 24 hours
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || len(authHeader) < 7 || authHeader[:7] != "Bearer " {
			http.Error(w, "Unauthorized: missing token", http.StatusUnauthorized)
			return
		}

		tokenString := authHeader[7:]

		token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userID := int(claims["user_id"].(float64))

		ctx := context.WithValue(r.Context(), "user_id", userID)
		next(w, r.WithContext(ctx))
	}
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
	_ = godotenv.Load()

	secret := os.Getenv("JWT_SECRET")
	jwtSecret = []byte(secret)

	connStr := os.Getenv("DATABASE_URL")
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

	http.HandleFunc("GET /websites", requireAuth(handleGetWebsites))
	http.HandleFunc("GET /websites/{id}", requireAuth(handleGetWebsiteByID))
	http.HandleFunc("POST /websites", requireAuth(handleCreateWebsite))
	http.HandleFunc("DELETE /websites/{id}", requireAuth(handleDeleteWebsite))
	http.HandleFunc("POST /auth/register", handleRegister)
	http.HandleFunc("POST /auth/login", handleLogin)

	go startBackgroundChecker()

	http.ListenAndServe(":8080", enableCORS(mux))
}
