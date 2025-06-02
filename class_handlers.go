package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	// "github.com/google/uuid" // Se for gerar UUIDs no Go, mas o DB já faz com gen_random_uuid()
)

// Struct Class (definida acima, mas coloque aqui ou importe de um arquivo de modelos)
type Class struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Payload para criar uma turma
type CreateClassPayload struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

// classRouterHandler para /classes e /classes/{id}
func classRouterHandler(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	path := r.URL.Path
	idSegment := strings.TrimPrefix(path, "/classes/")
	idSegment = strings.Trim(idSegment, "/")

	log.Printf("DEBUG: classRouterHandler: Path: %s, idSegment: '%s', Method: %s", path, idSegment, r.Method)

	if idSegment == "" { // Rota base: /classes/
		switch r.Method {
		case http.MethodPost:
			// Proteger com authMiddleware e depois chamar handleCreateClass
			authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				handleCreateClass(ww, rr, appDB)
			})).ServeHTTP(w, r)
		// case http.MethodGet:
		// TODO: handleGetClasses (listar todas as turmas)
		// http.Error(w, "GET /classes/ não implementado", http.StatusNotImplemented)
		default:
			http.Error(w, "Método não permitido para /classes/", http.StatusMethodNotAllowed)
		}
	} else { // Rota com ID: /classes/{id}
		classID := idSegment
		switch r.Method {
		// case http.MethodGet:
		// TODO: handleGetClassByID
		// case http.MethodPut:
		// TODO: handleUpdateClass
		// case http.MethodDelete:
		// TODO: handleDeleteClass
		default:
			http.Error(w, fmt.Sprintf("Método para /classes/%s não implementado ou não permitido", classID), http.StatusMethodNotAllowed)
		}
	}
}

func handleCreateClass(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	// 1. Verificar papel do usuário (ADMIN ou SUPER_ADMIN)
	userIDfromContext := r.Context().Value(userContextKey)
	requestingUserID := userIDfromContext.(string) // Assumindo que o middleware já validou e colocou

	requestingUserProfile, err := fetchUserProfile(requestingUserID, appDB) // Reutiliza a função de profile_handlers.go
	if err != nil {
		http.Error(w, "Erro ao verificar permissões do usuário.", http.StatusInternalServerError)
		return
	}

	if requestingUserProfile.Role != "admin" && requestingUserProfile.Role != "super_admin" {
		http.Error(w, "Acesso não autorizado para criar turmas.", http.StatusForbidden)
		return
	}

	// 2. Decodificar payload
	var payload CreateClassPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Payload inválido: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if strings.TrimSpace(payload.Name) == "" {
		http.Error(w, "Nome da turma é obrigatório.", http.StatusBadRequest)
		return
	}

	// 3. Inserir no banco
	var newClass Class
	sqlStatement := `
		INSERT INTO public.classes (name, description) VALUES ($1, $2)
		RETURNING id, name, description, created_at, updated_at`

	var dbDescription sql.NullString
	if payload.Description != nil {
		dbDescription.String = *payload.Description
		dbDescription.Valid = true
	}

	var scannedDescription sql.NullString // Variável para o Scan do description

	// Linha onde 'err' é atribuído pelo Scan
	err = appDB.QueryRow(sqlStatement, payload.Name, dbDescription).Scan(
		&newClass.ID,
		&newClass.Name,
		&scannedDescription, // Usa a variável para o scan
		&newClass.CreatedAt,
		&newClass.UpdatedAt,
	)
	if scannedDescription.Valid {
		newClass.Description = &scannedDescription.String
	}

	if err != nil {
		// Verificar erro de constraint UNIQUE para 'name'
		if strings.Contains(err.Error(), "duplicate key value violates unique constraint") ||
			strings.Contains(err.Error(), "classes_name_key") { // O nome da constraint pode variar
			http.Error(w, "Uma turma com este nome já existe.", http.StatusConflict) // 409 Conflict
		} else {
			log.Printf("Erro ao inserir turma no banco: %v", err)
			http.Error(w, "Erro ao criar turma.", http.StatusInternalServerError)
		}
		return // Importante retornar aqui se houve erro
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newClass)
}
