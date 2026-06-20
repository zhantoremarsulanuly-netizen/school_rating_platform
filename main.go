package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	_ "github.com/mattn/go-sqlite3"
)

// User представляет базовый цифровой профиль ученика
type User struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Class       string `json:"class"`
	Role        string `json:"role"` // Например: "Министр спорта", "Ученик", "Волонтер"
	TotalRating int    `json:"total_rating"`
}

// Achievement представляет отдельную запись о достижении ученика
type Achievement struct {
	ID       int    `json:"id"`
	UserID   int    `json:"user_id"`
	Category string `json:"category"` // "Спорт", "Академия", "Арт", "Волонтерство"
	Text     string `json:"text"`     // Описание подвига
	Points   int    `json:"points"`   // Количество баллов
}

var db *sql.DB

func main() {
	var err error
	// Инициализация реляционной БД SQLite
	db, err = sql.Open("sqlite3", "./nis_life.db")
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}
	defer db.Close()

	// Включаем поддержку Foreign Keys в SQLite (по умолчанию она выключена)
	_, _ = db.Exec("PRAGMA foreign_keys = ON;")

	// Инициализация таблиц
	initDatabase()

	// Раздача фронтенда из текущей директории
	fs := http.FileServer(http.Dir("./"))
	http.Handle("/", fs)

	// API Маршруты
	http.HandleFunc("/api/users", usersHandler)             // Получить топ учеников (с фильтрами) / Создать профиль
	http.HandleFunc("/api/achievements", achievementsHandler) // Добавить достижение / Начислить баллы
	http.HandleFunc("/api/qrcode", qrCodeHandler)           // Редирект на генератор QR для ученика

	fmt.Println("Сервер общешкольной платформы запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// Инициализация реляционной структуры таблиц
func initDatabase() {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			class TEXT NOT NULL,
			role TEXT DEFAULT 'Ученик'
		);`,
		`CREATE TABLE IF NOT EXISTS achievements (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			category TEXT NOT NULL,
			text TEXT NOT NULL,
			points INTEGER NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
	}

	for _, query := range tables {
		statement, err := db.Prepare(query)
		if err != nil {
			log.Fatal("Ошибка подготовки структуры БД:", err)
		}
		statement.Exec()
	}
}

// Handler для работы с пользователями (GET с фильтрацией и POST)
func usersHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		// Чтение параметров фильтрации из URL (например, ?class=9A или ?category=Спорт)
		filterClass := r.URL.Query().Get("class")
		filterCategory := r.URL.Query().Get("category")

		// Конструируем SQL-запрос с автоматическим расчетом суммы баллов (SUM) через LEFT JOIN
		query := `
			SELECT u.id, u.name, u.class, u.role, COALESCE(SUM(a.points), 0) as total_rating
			FROM users u
			LEFT JOIN achievements a ON u.id = a.user_id
		`
		var args []interface{}
		whereAdded := false

		// Динамически добавляем фильтр по классу
		if filterClass != "" {
			query += " WHERE u.class = ?"
			args = append(args, filterClass)
			whereAdded = true
		}

		// Динамически добавляем фильтр по категории достижений
		if filterCategory != "" {
			if whereAdded {
				query += " AND a.category = ?"
			} else {
				query += " WHERE a.category = ?"
			}
			args = append(args, filterCategory)
		}

		// Группировка и жесткая сортировка по убыванию рейтинга (Зал Славы)
		query += " GROUP BY u.id ORDER BY total_rating DESC"

		rows, err := db.Query(query, args...)
		if err != nil {
			http.Error(w, "Ошибка выполнения запроса в БД", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var users []User = []User{}
		for rows.Next() {
			var u User
			if err := rows.Scan(&u.ID, &u.Name, &u.Class, &u.Role, &u.TotalRating); err != nil {
				http.Error(w, "Ошибка сканирования данных", http.StatusInternalServerError)
				return
			}
			users = append(users, u)
		}
		json.NewEncoder(w).Encode(users)

	case http.MethodPost:
		var u User
		if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
			http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
			return
		}

		stmt, _ := db.Prepare("INSERT INTO users (name, class, role) VALUES (?, ?, ?)")
		result, err := stmt.Exec(u.Name, u.Class, u.Role)
		if err != nil {
			http.Error(w, "Ошибка сохранения пользователя", http.StatusInternalServerError)
			return
		}

		id, _ := result.LastInsertId()
		u.ID = int(id)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(u)

	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

// Handler для начисления достижений/баллов ученикам (POST)
func achievementsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, "Разрешен только POST метод", http.StatusMethodNotAllowed)
		return
	}

	var a Achievement
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		http.Error(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	// Проверяем существование пользователя перед добавлением достижения (защита целостности)
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", a.UserID).Scan(&exists)
	if err != nil || !exists {
		http.Error(w, "Ученик с таким ID не найден в системе", http.StatusNotFound)
		return
	}

	stmt, _ := db.Prepare("INSERT INTO achievements (user_id, category, text, points) VALUES (?, ?, ?, ?)")
	result, err := stmt.Exec(a.UserID, a.Category, a.Text, a.Points)
	if err != nil {
		http.Error(w, "Ошибка записи достижения", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	a.ID = int(id)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(a)
}

// Handler для генерации QR-кодов (принимает id пользователя)
func qrCodeHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("id")
	if userID == "" {
		http.Error(w, "Параметр 'id' обязателен", http.StatusBadRequest)
		return
	}

	// Ссылка на профиль конкретного ученика для сканирования мобильным
	profileURL := fmt.Sprintf("http://localhost:8080/?search_id=%s", userID)
	apiURL := "https://api.qrserver.com/v1/create-qr-code/?size=256x256&data=" + strconv.Quote(profileURL)

	http.Redirect(w, r, apiURL, http.StatusSeeOther)
}