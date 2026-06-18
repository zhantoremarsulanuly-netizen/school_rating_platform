package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Athlete struct {
	Name   string `json:"name"`
	Class  string `json:"class"`
	Sport  string `json:"sport"`
	Rating int    `json:"rating"`
}

var athletes = []Athlete{
	{Name: "Алихан Смаилов", Class: "9A", Sport: "Футбол", Rating: 150},
	{Name: "Данияр Маратов", Class: "10B", Sport: "Баскетбол", Rating: 120},
}

func main() {
	fs := http.FileServer(http.Dir("./"))
	http.Handle("/", fs)

	// Обработчик API для спортсменов (получение и добавление)
	http.HandleFunc("/api/athletes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost {
			// Если пришел POST-запрос — значит, добавляем нового спортсмена
			var newAthlete Athlete
			err := json.NewDecoder(r.Body).Decode(&newAthlete)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Добавляем в наш массив в памяти
			athletes = append(athletes, newAthlete)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(newAthlete)
			return
		}

		// Если пришел GET-запрос — просто отдаем список
		json.NewEncoder(w).Encode(athletes)
	})

	fmt.Println("Сервер запущен на http://localhost:8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Ошибка запуска сервера:", err)
	}
}