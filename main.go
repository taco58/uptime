package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type Result struct {
	URL          string        `json:"url"`
	Status       int           `json:"status"`
	Latency      time.Duration `json:"latency"`
	Success      bool          `json:"success"`
	ErrorMessage string        `json:"error_message,omitempty"`
}

type Website struct {
	ID         int     `json:"id"`
	URL        string  `json:"url"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	WebhookURL *string `json:"webhook_url,omitempty"`
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

type CheckJob struct {
	MonitorID      int     `json:"monitor_id"`
	URL            string  `json:"url"`
	WebhookURL     *string `json:"webhook_url,omitempty"`
	Name           string  `json:"name"`
	PreviousStatus string  `json:"previous_status"`
}

type DiscordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type DiscordEmbed struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Color       int            `json:"color"`
	Fields      []DiscordField `json:"fields"`
	Timestamp   string         `json:"timestamp"`
}

type DiscordPayload struct {
	Username  string         `json:"username"`
	AvatarURL string         `json:"avatar_url"`
	Embeds    []DiscordEmbed `json:"embeds"`
}

var db *sql.DB
var jwtSecret []byte
var rdb *redis.Client

func initRedis() {
	redisAddr := os.Getenv("REDIS_ADDR")

	rdb = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Could not connect to redis: %v", err)
	}

	fmt.Println("Connected to redis successfully")
}

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

func sendDiscordAlert(webhookURL string, job CheckJob, res Result, newStatus string) {
	color := 15158332
	title := fmt.Sprintf("🚨 Incident Detected: %s is DOWN", job.Name)
	desc := fmt.Sprintf("Target URL %s is unreachable or returned an error.", job.URL)

	if newStatus == "up" {
		color = 3066993
		title = fmt.Sprintf("✅ Incident Resolved: %s is BACK UP", job.Name)
		desc = fmt.Sprintf("Target URL %s is responding normally.", job.URL)
	}

	fields := []DiscordField{
		{Name: "Status Code", Value: fmt.Sprintf("%d", res.Status), Inline: true},
		{Name: "Latency", Value: fmt.Sprintf("%d ms", res.Latency.Milliseconds()), Inline: true},
		{Name: "Checked At", Value: time.Now().Format("15:04:05 MST"), Inline: true},
	}

	if res.ErrorMessage != "" {
		fields = append(fields, DiscordField{
			Name:   "Reason",
			Value:  res.ErrorMessage,
			Inline: false,
		})
	}

	payload := DiscordPayload{
		Username: "WatchDawg",
		Embeds: []DiscordEmbed{
			{
				Title:       title,
				Description: desc,
				Color:       color,
				Fields:      fields,
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal discord payload: %v\n", err)
		return
	}

	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		log.Printf("Failed to send discord webhook: %v\n", err)
		return
	}
	defer resp.Body.Close()

	log.Printf("Discord alert sent for %s (Status: %s)\n", job.Name, newStatus)
}

func checkSite(url string) Result {
	start := time.Now()

	client := http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)

	if err != nil {
		return Result{
			URL:          url,
			Latency:      time.Since(start),
			Success:      false,
			ErrorMessage: "Invalid URL format",
		}
	}

	req.Header.Set("User-Agent", "WatchDawg/1.0 (+https://github.com/taco58/uptime)")

	resp, err := client.Do(req)
	duration := time.Since(start)

	if err != nil {
		errMsg := "Network Error"

		var netErr net.Error
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			errMsg = fmt.Sprintf("DNS Lookup Failed (%s)", dnsErr.Name)
		} else if errors.As(err, &netErr) && netErr.Timeout() {
			errMsg = "Connection Timed Out (> 5000ms)"
		} else {
			errMsg = err.Error()
		}

		return Result{
			URL:          url,
			Latency:      duration,
			Success:      false,
			ErrorMessage: errMsg,
		}
	}

	defer resp.Body.Close()

	isSuccess := resp.StatusCode >= 200 && resp.StatusCode <= 399
	var errMsg string
	if !isSuccess {
		errMsg = fmt.Sprintf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	return Result{
		URL:          url,
		Status:       resp.StatusCode,
		Latency:      duration,
		Success:      isSuccess,
		ErrorMessage: errMsg,
	}

}

func checkSiteWithRetry(url string, maxRetries int) Result {
	var res Result
	for attempt := 1; attempt <= maxRetries; attempt++ {
		res = checkSite(url)
		if res.Success {
			return res
		}

		if attempt < maxRetries {
			backoff := time.Duration(attempt) * time.Second
			time.Sleep(backoff)
		}
	}

	return res
}

func handleGetWebsites(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(int)

	rows, err := db.Query("SELECT id, name, url, status, webhook_url FROM monitors WHERE user_id = $1", userID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list = []Website{}
	for rows.Next() {
		var site Website
		if err := rows.Scan(&site.ID, &site.Name, &site.URL, &site.Status, &site.WebhookURL); err != nil {
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

	err = db.QueryRow("INSERT INTO monitors (name, url, status, user_id, webhook_url) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		newSite.Name, newSite.URL, "unknown", userID, newSite.WebhookURL).Scan(&newSite.ID)

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
	err = db.QueryRow("SELECT id, name, url, status, webhook_url FROM monitors where id = $1 AND user_id = $2", id, userID).Scan(&site.ID, &site.Name, &site.URL, &site.Status, &site.WebhookURL)

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

// func checkAllMonitors() {
// 	rows, err := db.Query("SELECT id, url FROM monitors")

// 	if err != nil {
// 		fmt.Println(err)
// 		return
// 	}
// 	defer rows.Close()

// 	var wg sync.WaitGroup

// 	for rows.Next() {
// 		var site Website
// 		if err := rows.Scan(&site.ID, &site.URL); err != nil {
// 			fmt.Println(err)
// 			return
// 		}
// 		wg.Add(1)

// 		go func(s Website) {
// 			defer wg.Done()

// 			res := checkSite(s.URL)

// 			db.Exec("INSERT INTO checks (monitor_id, status_code, latency_ms, success) VALUES ($1, $2, $3, $4)", s.ID, res.Status, res.Latency.Milliseconds(), res.Success)

// 			status := "down"

// 			if res.Success {
// 				status = "up"
// 			}

// 			db.Exec("UPDATE monitors SET status = $1 WHERE id = $2", status, s.ID)
// 		}(site)
// 	}

// 	wg.Wait()

// 	if rows.Err() != nil {
// 		fmt.Println(err)
// 		return
// 	}
// }

// func startBackgroundChecker() {
// 	ticker := time.NewTicker(10 * time.Second)

// 	for range ticker.C {
// 		checkAllMonitors()
// 	}
// }

func scheduleChecks() {
	ctx := context.Background()
	rows, err := db.Query("SELECT id, name, url, status, webhook_url FROM monitors")
	if err != nil {
		log.Println("Error querying monitors:", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var job CheckJob
		if err := rows.Scan(&job.MonitorID, &job.Name, &job.URL, &job.PreviousStatus, &job.WebhookURL); err != nil {
			log.Println("Error scanning monitor:", err)
			continue
		}

		jobBytes, _ := json.Marshal(job)

		err = rdb.LPush(ctx, "check_queue", string(jobBytes)).Err()
		if err != nil {
			log.Println("Failed to enqueue job:", err)
		}
	}

	if rows.Err() != nil {
		log.Println("Error querying monitors:", err)
		return
	}
}

func startWorker(workerID int) {
	ctx := context.Background()
	fmt.Printf("Worker %d started\n", workerID)

	for {
		result, err := rdb.BRPop(ctx, 0, "check_queue").Result()
		if err != nil {
			log.Printf("Worker %d error popping job: %v\n", workerID, err)
			continue
		}

		var job CheckJob
		if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
			log.Printf("Worker %d failed to parse job: %v\n", workerID, err)
			continue
		}

		res := checkSiteWithRetry(job.URL, 2)

		_, err = db.Exec(
			"INSERT INTO checks (monitor_id, status_code, latency_ms, success, error_message) VALUES ($1, $2, $3, $4, $5)",
			job.MonitorID, res.Status, res.Latency.Milliseconds(), res.Success, res.ErrorMessage,
		)
		if err != nil {
			log.Printf("Worker %d failed to save check: %v\n", workerID, err)
		}

		status := "down"
		if res.Success {
			status = "up"
		}

		if job.PreviousStatus != "unknown" && job.PreviousStatus != status && job.WebhookURL != nil && *job.WebhookURL != "" {
			go sendDiscordAlert(*job.WebhookURL, job, res, status)
		}

		_, _ = db.Exec("UPDATE monitors SET status = $1 WHERE id = $2", status, job.MonitorID)

		fmt.Printf("Worker %d checked %s -> %s (%dms)\n", workerID, job.URL, status, res.Latency.Milliseconds())
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

	initRedis()

	http.HandleFunc("GET /websites", requireAuth(handleGetWebsites))
	http.HandleFunc("GET /websites/{id}", requireAuth(handleGetWebsiteByID))
	http.HandleFunc("POST /websites", requireAuth(handleCreateWebsite))
	http.HandleFunc("DELETE /websites/{id}", requireAuth(handleDeleteWebsite))
	http.HandleFunc("POST /auth/register", handleRegister)
	http.HandleFunc("POST /auth/login", handleLogin)

	for i := 1; i <= 3; i++ {
		go startWorker(i)
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for range ticker.C {
			scheduleChecks()
		}
	}()

	http.ListenAndServe(":8080", enableCORS(mux))
}
