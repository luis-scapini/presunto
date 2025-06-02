// astera/cantina-service/profile_handlers.go
package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// UserProfile representa os dados do perfil do usuário que retornaremos na API
type UserProfile struct {
	ID        string    `json:"id"`
	FullName  *string   `json:"full_name,omitempty"` // Usamos ponteiros para campos que podem ser nulos
	Email     *string   `json:"email,omitempty"`
	Credits   *float64  `json:"credits,omitempty"` // NUMERIC(10,2) pode ser float64
	Role      string    `json:"role"`              // Role agora é NOT NULL
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// handleGetMyProfile busca e retorna o perfil do usuário autenticado
func handleGetMyProfile(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	// O userID é injetado no contexto pelo authMiddleware
	userIDfromContext := r.Context().Value(userContextKey)
	if userIDfromContext == nil {
		log.Println("Erro: userID não encontrado no contexto da requisição.")
		http.Error(w, "Usuário não autenticado ou ID não encontrado no token", http.StatusUnauthorized)
		return
	}

	userID, ok := userIDfromContext.(string)
	if !ok || userID == "" {
		log.Println("Erro: userID no contexto não é uma string válida ou está vazio.")
		http.Error(w, "ID de usuário inválido no token", http.StatusUnauthorized)
		return
	}

	log.Printf("Buscando perfil para o usuário ID: %s", userID)

	var profile UserProfile
	// Campos que podem ser nulos no banco
	var fullName sql.NullString
	var email sql.NullString
	var credits sql.NullFloat64 // Para NUMERIC que pode ser nulo (embora o nosso tenha DEFAULT)

	sqlStatement := `
		SELECT id, full_name, email, credits, role, created_at, updated_at 
		FROM public.users 
		WHERE id = $1;`

	row := appDB.QueryRow(sqlStatement, userID)
	err := row.Scan(
		&profile.ID,
		&fullName,
		&email,
		&credits,
		&profile.Role, // Role é NOT NULL, então não precisa de sql.NullString
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("Perfil não encontrado para o usuário ID: %s", userID)
			http.Error(w, "Perfil do usuário não encontrado", http.StatusNotFound)
		} else {
			log.Printf("Erro ao buscar perfil para usuário ID (%s): %v", userID, err)
			http.Error(w, "Erro no servidor ao buscar perfil", http.StatusInternalServerError)
		}
		return
	}

	// Atribuir valores dos tipos Null para os ponteiros no struct UserProfile
	if fullName.Valid {
		profile.FullName = &fullName.String
	}
	if email.Valid {
		profile.Email = &email.String
	}
	if credits.Valid {
		profile.Credits = &credits.Float64
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

func fetchUserProfile(userID string, appDB *sql.DB) (*UserProfile, error) {
	var profile UserProfile
	var fullName, email sql.NullString // Role é NOT NULL, credits tem DEFAULT
	var credits sql.NullFloat64        // Se credits puder ser NULL no DB

	sqlStatement := `
		SELECT id, full_name, email, credits, role, created_at, updated_at 
		FROM public.users 
		WHERE id = $1;`

	row := appDB.QueryRow(sqlStatement, userID)
	err := row.Scan(
		&profile.ID,
		&fullName,
		&email,
		&credits,
		&profile.Role,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)

	if err != nil {
		return nil, err // Retorna o erro diretamente (incluindo sql.ErrNoRows)
	}

	if fullName.Valid {
		profile.FullName = &fullName.String
	}
	if email.Valid {
		profile.Email = &email.String
	}
	if credits.Valid {
		profile.Credits = &credits.Float64
	}
	return &profile, nil
}
