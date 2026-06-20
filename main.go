package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	// Ультимативный драйвер: 100% Pure Go. Работает без CGO и внешних компиляторов!
	_ "modernc.org/sqlite"
)

// Global DB connection
var db *sql.DB

// --- МОДЕЛИ ДАННЫХ ДЛЯ СУБД ---
type User struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Class       string `json:"class"`
	Role        string `json:"role"`
	TotalRating int    `json:"total_rating"`
}

type Achievement struct {
	ID       int    `json:"id"`
	UserID   int    `json:"user_id"`
	Category string `json:"category"`
	Text     string `json:"text"`
	Points   int    `json:"points"`
}

// --- АВТОМАТИЧЕСКАЯ ИНИЦИАЛИЗАЦИЯ БАЗЫ ДАННЫХ ---
func initDatabase() {
	// Таблица цифровых профилей учеников
	userTable := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT EXISTS,
		class TEXT NOT EXISTS,
		role TEXT DEFAULT 'Ученик'
	);`

	// Таблица достижений (Реляционная связь Один-ко-Многим с каскадным удалением)
	achievementTable := `
	CREATE TABLE IF NOT EXISTS achievements (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT EXISTS,
		category TEXT NOT EXISTS,
		text TEXT NOT EXISTS,
		points INTEGER NOT EXISTS,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);`

	_, err := db.Exec(userTable)
	if err != nil {
		log.Fatal("Ошибка создания таблицы пользователей:", err)
	}

	_, err = db.Exec(achievementTable)
	if err != nil {
		log.Fatal("Ошибка создания таблицы достижений:", err)
	}
}

// --- ОБРАБОТЧИК ДЛЯ УПРАВЛЕНИЯ ПОЛЬЗОВАТЕЛЯМИ (/api/users) ---
func usersHandler(w http.ResponseWriter, r *http.Request) {
	// Настройка заголовков CORS безопасности для связи с фронтендом
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Admin-Token")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 1. ПОЛУЧЕНИЕ ДАННЫХ (Разрешено всем ученикам школы)
	if r.Method == "GET" {
		category := r.URL.Query().Get("category")
		className := r.URL.Query().Get("class")

		// Динамический SQL-запрос с автоматическим расчетом агрегированных баллов (SUM)
		query := `
			SELECT u.id, u.name, u.class, u.role, COALESCE(SUM(a.points), 0) as total_rating
			FROM users u
			LEFT JOIN achievements a ON u.id = a.user_id `

		var args []interface{}
		whereClauses := []string{}

		// Интерактивная фильтрация по категориям (лигам) на уровне СУБД
		if category != "" {
			whereClauses = append(whereClauses, "a.category = ?")
			args = append(args, category)
		}
		// Интерактивная фильтрация по параллелям классов
		if className != "" {
			whereClauses = append(whereClauses, "u.class = ?")
			args = append(args, className)
		}

		if len(whereClauses) > 0 {
			query += " WHERE "
			for i, clause := range whereClauses {
				if i > 0 {
					query += " AND "
				}
				query += clause
			}
		}

		// Группировка и жесткая сортировка для вывода лидеров на Пьедестал
		query += " GROUP BY u.id ORDER BY total_rating DESC"

		rows, err := db.Query(query, args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var users []User
		for rows.Next() {
			var u User
			if err := rows.Scan(&u.ID, &u.Name, &u.Class, &u.Role, &u.TotalRating); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			users = append(users, u)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)
		return
	}

	// 2. ЗАПИСЬ ДАННЫХ (Только для роли АДМИНИСТРАТОРА)
	if r.Method == "POST" {
		// Защита уровня бэкенда: проверка секретного токена
		token := r.Header.Get("X-Admin-Token")
		if token != "nis_admin_2026" {
			http.Error(w, "Status 403 Forbidden: Отказано в доступе", http.StatusForbidden)
			return
		}

		var u User
		if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, err := db.Exec("INSERT INTO users (name, class, role) VALUES (?, ?, ?)", u.Name, u.Class, u.Role)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "created"})
		return
	}
}

// --- ОБРАБОТЧИК НАЧИСЛЕНИЯ БАЛЛОВ (/api/achievements) ---
func achievementsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Admin-Token")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == "POST" {
		// Защита уровня бэкенда: проверка токена администратора перед транзакцией в СУБД
		token := r.Header.Get("X-Admin-Token")
		if token != "nis_admin_2026" {
			http.Error(w, "Status 403 Forbidden: Накрутка баллов заблокирована", http.StatusForbidden)
			return
		}

		var a Achievement
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Реляционный контроль: Проверяем, существует ли ученик с таким ID в users
		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", a.UserID).Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Сбой связности: Ученик не существует", http.StatusNotFound)
			return
		}

		// Запись транзакции начисления баллов
		_, err = db.Exec("INSERT INTO achievements (user_id, category, text, points) VALUES (?, ?, ?, ?)",
			a.UserID, a.Category, a.Text, a.Points)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "points_added"})
		return
	}
}

// --- ОБРАБОТЧИК ДИНАМИЧЕСКИХ QR-КОДОВ (/api/qrcode) ---
func qrCodeHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing ID parameters", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Формируем ссылку обратного сканирования верификации (подсветка при наведении камеры смартфона)
	targetURL := fmt.Sprintf("http://%s/?search_id=%d", r.Host, id)
	
	// Внешнее ультра-легкое API генерации QR-матрицы без скачивания тяжелых библиотек
	qrProvider := fmt.Sprintf("https://api.qrserver.com/v1/create-qr-code/?size=220x220&data=%s", targetURL)

	// Перенаправляем поток картинки сразу в браузер клиента
	http.Redirect(w, r, qrProvider, http.StatusFound)
}

// --- ГЛАВНАЯ СЛУЖЕБНАЯ ФУНКЦИЯ ЗАПУСКА ЯДРА ---
func main() {
	var err error
	
	// Подключаем базу данных с использованием нового Pure Go драйвера ("sqlite")
	db, err = sql.Open("sqlite", "./nis_life.db")
	if err != nil {
		log.Fatal("Критический сбой инициализации базы данных:", err)
	}
	defer db.Close()

	// Принудительно включаем каскадные ключи для реляционных связей в SQLite
	_, _ = db.Exec("PRAGMA foreign_keys = ON;")

	// Проверяем и разворачиваем таблицы, если файла nis_life.db еще нет на диске
	initDatabase()

	// Подключаем автоматическую раздачу статического фронтенда (index.html) из корня проекта
	fs := http.FileServer(http.Dir("./"))
	http.Handle("/", fs)

	// Регистрация защищенных сетевых маршрутов API
	http.HandleFunc("/api/users", usersHandler)
	http.HandleFunc("/api/achievements", achievementsHandler)
	http.HandleFunc("/api/qrcode", qrCodeHandler)

	// Умный выбор порта: Считываем системный порт хостинга (для деплоя), либо падаем на 8080 (локально)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("🚀 [NIS ID ERP Engine v2]: Сервер успешно запущен на порту %s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}