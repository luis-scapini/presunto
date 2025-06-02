package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings" // NOVO: Para manipular strings (vamos usar para pegar o ID da URL)
	"time"
	// _ "github.com/lib/pq" // Já importado no main.go ou aqui se preferir
)

// MenuItem struct (sem mudanças)
type MenuItem struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	Price       float64   `json:"price"`
	Category    *string   `json:"category,omitempty"`
	ImageURL    *string   `json:"image_url,omitempty"`
	IsAvailable bool      `json:"is_available"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateMenuItemPayload struct (sem mudanças)
type CreateMenuItemPayload struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Price       float64 `json:"price"`
	Category    *string `json:"category"`
	ImageURL    *string `json:"image_url"`
	IsAvailable *bool   `json:"is_available"`
}

// menuItemsRouterHandler decide qual função chamar baseado no método HTTP e no PATH
// Dentro de menu_handlers.go

func menuItemsRouterHandler(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	log.Printf("DEBUG: menuItemsRouterHandler: Recebido path: %s, Método: %s", r.URL.Path, r.Method)

	itemID := strings.TrimPrefix(r.URL.Path, "/menu-items/")
	log.Printf("DEBUG: menuItemsRouterHandler: Calculado itemID: '%s'", itemID)

	if itemID == "" { // Rota base: /menu-items/
		switch r.Method {
		case http.MethodGet:
			handleGetMenuItems(w, r, appDB) // Listar todos - PÚBLICO
		case http.MethodPost:
			// NOVO: Aplicando o middleware de autenticação ANTES de chamar handleCreateMenuItem
			authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				// O userID está no contexto rr.Context().Value(userContextKey) se precisar dele aqui
				handleCreateMenuItem(ww, rr, appDB)
			})).ServeHTTP(w, r) // Importante: ServeHTTP(w,r) original
		default:
			http.Error(w, "Método não permitido para /menu-items/", http.StatusMethodNotAllowed)
		}
	} else { // Rota com ID: /menu-items/{id}
		// Por enquanto, vamos manter GET /{id} público e proteger PUT e DELETE
		switch r.Method {
		case http.MethodGet:
			handleGetMenuItemByID(w, r, appDB, itemID) // PÚBLICO
		case http.MethodPut:
			// NOVO: Aplicando o middleware
			authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				handleUpdateMenuItem(ww, rr, appDB, itemID)
			})).ServeHTTP(w, r)
		case http.MethodDelete:
			// NOVO: Aplicando o middleware
			authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				handleDeleteMenuItem(ww, rr, appDB, itemID)
			})).ServeHTTP(w, r)
		default:
			http.Error(w, "Método não permitido para /menu-items/{id}", http.StatusMethodNotAllowed)
		}
	}
}

// handleGetMenuItems (sem mudanças, lista todos)
func handleGetMenuItems(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	rows, err := appDB.Query("SELECT id, name, description, price, category, image_url, is_available, created_at, updated_at FROM public.menu_items ORDER BY name ASC")
	if err != nil {
		log.Printf("Erro ao buscar itens do cardápio: %v", err)
		http.Error(w, "Erro ao buscar dados do servidor", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	menu := []MenuItem{}
	for rows.Next() {
		var item MenuItem
		var description, category, imageURL sql.NullString
		err := rows.Scan(&item.ID, &item.Name, &description, &item.Price, &category, &imageURL, &item.IsAvailable, &item.CreatedAt, &item.UpdatedAt)
		if err != nil { /* ... tratamento de erro ... */
			http.Error(w, "Erro processar", http.StatusInternalServerError)
			return
		}
		if description.Valid {
			item.Description = &description.String
		}
		if category.Valid {
			item.Category = &category.String
		}
		if imageURL.Valid {
			item.ImageURL = &imageURL.String
		}
		menu = append(menu, item)
	}
	if err = rows.Err(); err != nil { /* ... tratamento de erro ... */
		http.Error(w, "Erro dados", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(menu)
}

// handleCreateMenuItem (sem mudanças, cria um novo)
func handleCreateMenuItem(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	var payload CreateMenuItemPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil { /* ... */
		http.Error(w, "Payload inválido", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	if payload.Name == "" { /* ... */
		http.Error(w, "Nome obrigatório", http.StatusBadRequest)
		return
	}
	if payload.Price <= 0 { /* ... */
		http.Error(w, "Preço > 0", http.StatusBadRequest)
		return
	}
	isAvailable := true
	if payload.IsAvailable != nil {
		isAvailable = *payload.IsAvailable
	}

	var newItem MenuItem
	sqlStatement := `INSERT INTO public.menu_items (name, description, price, category, image_url, is_available)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, name, description, price, category, image_url, is_available, created_at, updated_at`
	row := appDB.QueryRow(sqlStatement, payload.Name, payload.Description, payload.Price, payload.Category, payload.ImageURL, isAvailable)
	var dbDesc, dbCat, dbImg sql.NullString
	err := row.Scan(&newItem.ID, &newItem.Name, &dbDesc, &newItem.Price, &dbCat, &dbImg, &newItem.IsAvailable, &newItem.CreatedAt, &newItem.UpdatedAt)
	if err != nil { /* ... */
		log.Printf("Erro DB Insert/Scan: %v", err)
		http.Error(w, "Erro servidor", http.StatusInternalServerError)
		return
	}
	if dbDesc.Valid {
		newItem.Description = &dbDesc.String
	}
	if dbCat.Valid {
		newItem.Category = &dbCat.String
	}
	if dbImg.Valid {
		newItem.ImageURL = &dbImg.String
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newItem)
}

// --- NOVAS FUNÇÕES HANDLER ---

// handleGetMenuItemByID busca um item específico pelo ID
func handleGetMenuItemByID(w http.ResponseWriter, r *http.Request, appDB *sql.DB, itemID string) {
	var item MenuItem
	sqlStatement := `SELECT id, name, description, price, category, image_url, is_available, created_at, updated_at 
					 FROM public.menu_items WHERE id = $1;`

	row := appDB.QueryRow(sqlStatement, itemID)
	var description, category, imageURL sql.NullString
	err := row.Scan(&item.ID, &item.Name, &description, &item.Price, &category, &imageURL, &item.IsAvailable, &item.CreatedAt, &item.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Item do cardápio não encontrado", http.StatusNotFound)
		} else {
			log.Printf("Erro ao buscar item por ID (%s): %v", itemID, err)
			http.Error(w, "Erro no servidor", http.StatusInternalServerError)
		}
		return
	}

	if description.Valid {
		item.Description = &description.String
	}
	if category.Valid {
		item.Category = &category.String
	}
	if imageURL.Valid {
		item.ImageURL = &imageURL.String
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(item)
}

// handleUpdateMenuItem atualiza um item existente pelo ID
func handleUpdateMenuItem(w http.ResponseWriter, r *http.Request, appDB *sql.DB, itemID string) {
	var payload CreateMenuItemPayload // Reutilizando o payload de criação para os campos que podem ser atualizados
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Payload inválido: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Poderíamos adicionar validações aqui também, como no create

	isAvailable := true // Valor padrão se não vier no payload
	if payload.IsAvailable != nil {
		isAvailable = *payload.IsAvailable
	}

	var updatedItem MenuItem
	sqlStatement := `
		UPDATE public.menu_items 
		SET name = $1, description = $2, price = $3, category = $4, image_url = $5, is_available = $6, updated_at = NOW()
		WHERE id = $7
		RETURNING id, name, description, price, category, image_url, is_available, created_at, updated_at;`

	row := appDB.QueryRow(sqlStatement, payload.Name, payload.Description, payload.Price, payload.Category, payload.ImageURL, isAvailable, itemID)

	var description, category, imageURL sql.NullString
	err := row.Scan(
		&updatedItem.ID, &updatedItem.Name, &description, &updatedItem.Price, &category, &imageURL,
		&updatedItem.IsAvailable, &updatedItem.CreatedAt, &updatedItem.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Item do cardápio não encontrado para atualização", http.StatusNotFound)
		} else {
			log.Printf("Erro ao atualizar item (%s): %v", itemID, err)
			http.Error(w, "Erro no servidor ao atualizar", http.StatusInternalServerError)
		}
		return
	}

	if description.Valid {
		updatedItem.Description = &description.String
	}
	if category.Valid {
		updatedItem.Category = &category.String
	}
	if imageURL.Valid {
		updatedItem.ImageURL = &imageURL.String
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedItem)
}

// handleDeleteMenuItem deleta um item pelo ID
func handleDeleteMenuItem(w http.ResponseWriter, r *http.Request, appDB *sql.DB, itemID string) {
	sqlStatement := `DELETE FROM public.menu_items WHERE id = $1 RETURNING id;`

	var deletedID string
	err := appDB.QueryRow(sqlStatement, itemID).Scan(&deletedID)

	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Item do cardápio não encontrado para deleção", http.StatusNotFound)
		} else {
			log.Printf("Erro ao deletar item (%s): %v", itemID, err)
			http.Error(w, "Erro no servidor ao deletar", http.StatusInternalServerError)
		}
		return
	}

	log.Printf("Item deletado com sucesso: %s", deletedID)
	w.WriteHeader(http.StatusNoContent) // 204 No Content é uma boa resposta para DELETE bem-sucedido
}
