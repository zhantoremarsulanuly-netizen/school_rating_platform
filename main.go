package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Athlete описывает структуру данных одного спортсмена
type Athlete struct {
	Name   string `json:"name"`
	Class  string `json:"class"`
	Sport  string `json:"sport"`
	Rating int    `json:"rating"`
}

// Временная база данных прямо в коде
var athletes = []Athlete{
	{Name: "Алихан Смаилов", Class: "9A", Sport: "Футбол", Rating: 150},
	{Name: "Данияр Маратов", Class: "10B", Sport: "Баскетбол", Rating: 120},
	{Name: "Малика Ибрагимова", Class: "9A", Sport: "Волейбол", Rating: 140},
}

func main() {
	// Главная страница
	fs := http.FileServer(http.Dir("./"))
	http.Handle("/", fs)

	// API, которое будет отдавать список спортсменов в формате JSON
	http.HandleFunc("/api/athletes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(athletes)
	})

	fmt.Println("Сервер запущен на http://localhost:8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Ошибка запуска сервера:", err)
	}
}