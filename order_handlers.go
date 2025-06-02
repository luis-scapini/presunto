// astera/cantina-service/order_handlers.go
package main

import (

	// NOVO: Para criar erros personalizados

	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	// A importação do driver _ "github.com/lib/pq" já está no main.go
)

// OrderItemRequest representa um item dentro de um pedido na requisição de criação
type OrderItemRequest struct {
	MenuItemID string `json:"menu_item_id"`
	Quantity   int    `json:"quantity"`
}

// CreateOrderRequest representa o payload para criar um novo pedido
type CreateOrderRequest struct {
	Items     []OrderItemRequest `json:"items"`
	StudentID string             `json:"student_id"` // NOVO e OBRIGATÓRIO
	// Turma *string `json:"turma,omitempty"` // REMOVA ESTE CAMPO SE VOCÊ O TINHA ANTES
}

// OrderItem (para respostas e uso interno, espelha a tabela order_items)
type OrderItem struct {
	ID              string    `json:"id"`
	OrderID         string    `json:"order_id"`
	MenuItemID      string    `json:"menu_item_id"`
	MenuItemName    string    `json:"menu_item_name,omitempty"` // NOVO CAMPO
	Quantity        int       `json:"quantity"`
	PriceAtPurchase float64   `json:"price_at_purchase"`
	CreatedAt       time.Time `json:"created_at"`
	// Poderíamos adicionar 'name' do item aqui para facilitar no frontend, buscando com um JOIN
}

// Order (para respostas e uso interno, espelha a tabela orders)
type Order struct {
	ID          string      `json:"id"`
	UserID      string      `json:"user_id"`
	StudentID   *string     `json:"student_id,omitempty"` // NOVO: ID do aluno para quem é o pedido
	OrderDate   time.Time   `json:"order_date"`
	TotalAmount float64     `json:"total_amount"`
	Status      string      `json:"status"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	Items       []OrderItem `json:"items,omitempty"` // Para incluir os itens do pedido na resposta

}

// Struct para o payload da requisição de atualização de status
type UpdateOrderStatusPayload struct {
	Status string `json:"status"`
}

// (continuação do arquivo order_handlers.go, abaixo das structs)

// handleUpdateOrderStatus atualiza o status de um pedido específico
func handleUpdateOrderStatus(w http.ResponseWriter, r *http.Request, appDB *sql.DB, orderID string) {
	// 1. Autenticação já foi feita. Pegar userID e perfil (para checar o papel).
	userIDfromContext := r.Context().Value(userContextKey)
	if userIDfromContext == nil {
		http.Error(w, "Usuário não autenticado", http.StatusUnauthorized)
		return
	}
	requestingUserID := userIDfromContext.(string)

	requestingUserProfile, err := fetchUserProfile(requestingUserID, appDB)
	if err != nil {
		// Tratar erro ao buscar perfil
		if err == sql.ErrNoRows {
			http.Error(w, "Perfil de usuário solicitante não encontrado.", http.StatusUnauthorized)
		} else {
			log.Printf("Erro ao buscar perfil do usuário %s: %v", requestingUserID, err)
			http.Error(w, "Erro no servidor ao verificar permissões.", http.StatusInternalServerError)
		}
		return
	}

	// 2. Autorização: Apenas STAFF, ADMIN, ou SUPER_ADMIN podem mudar status de pedidos.
	if requestingUserProfile.Role != "staff" && requestingUserProfile.Role != "admin" && requestingUserProfile.Role != "super_admin" {
		log.Printf("Usuário %s (Papel: %s) tentou atualizar status do pedido %s sem permissão.", requestingUserID, requestingUserProfile.Role, orderID)
		http.Error(w, "Acesso não autorizado para esta ação.", http.StatusForbidden)
		return
	}

	// 3. Decodificar o novo status do corpo da requisição
	var payload UpdateOrderStatusPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("Erro ao decodificar payload para atualizar status do pedido %s: %v", orderID, err)
		http.Error(w, "Corpo da requisição inválido: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 4. Validar o novo status (opcional, mas recomendado)
	//    Você pode querer uma lista de status válidos: e.g., "PREPARING", "READY", "COMPLETED", "CANCELED"
	//    E talvez regras de transição (ex: não pode ir de COMPLETED para PREPARING).
	//    Por simplicidade, vamos permitir qualquer string por enquanto, mas registre que isso deve ser validado.
	newStatus := strings.TrimSpace(payload.Status)
	if newStatus == "" {
		http.Error(w, "Novo status não pode ser vazio.", http.StatusBadRequest)
		return
	}
	// Exemplo de validação de status (pode expandir)
	validStatuses := map[string]bool{"PENDING": true, "PREPARING": true, "READY": true, "COMPLETED": true, "CANCELED": true}
	if !validStatuses[strings.ToUpper(newStatus)] {
		http.Error(w, fmt.Sprintf("Status '%s' inválido.", newStatus), http.StatusBadRequest)
		return
	}

	log.Printf("Usuário %s (Papel: %s) atualizando status do pedido %s para '%s'", requestingUserID, requestingUserProfile.Role, orderID, newStatus)

	// 5. Atualizar o status no banco de dados
	var updatedOrder Order // Para retornar o pedido atualizado completo
	updateQuery := `
		UPDATE public.orders 
		SET status = $1, updated_at = NOW() 
		WHERE id = $2
		RETURNING id, user_id, order_date, total_amount, status, created_at, updated_at;`

	err = appDB.QueryRow(updateQuery, strings.ToUpper(newStatus), orderID).Scan(
		&updatedOrder.ID,
		&updatedOrder.UserID,
		&updatedOrder.OrderDate,
		&updatedOrder.TotalAmount,
		&updatedOrder.Status,
		&updatedOrder.CreatedAt,
		&updatedOrder.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Pedido não encontrado para atualização de status.", http.StatusNotFound)
		} else {
			log.Printf("Erro ao atualizar status do pedido %s: %v", orderID, err)
			http.Error(w, "Erro no servidor ao atualizar status do pedido.", http.StatusInternalServerError)
		}
		return
	}

	// 6. Buscar os itens do pedido atualizado para retornar o objeto completo
	orderItems, errItems := fetchOrderItemsByOrderID(appDB, updatedOrder.ID)
	if errItems != nil {
		log.Printf("Alerta: Não foi possível buscar itens para o pedido atualizado %s: %v", updatedOrder.ID, errItems)
		updatedOrder.Items = []OrderItem{} // Retorna com itens vazios se houver erro aqui
	} else {
		updatedOrder.Items = orderItems
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedOrder)
}

// handleCreateOrder cria um novo pedido
func handleCreateOrder(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	userIDfromContext := r.Context().Value(userContextKey).(string) // ID do pai/responsável logado
	// (Opcional, mas bom: buscar perfil do pai para pegar o role, caso queira restringir quem pode criar pedidos)
	//requestingUserProfile, _ := fetchUserProfile(userIDfromContext, appDB)
	// if requestingUserProfile.Role != "CLIENTE" { ... erro ... }

	var reqPayload CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&reqPayload); err != nil {
		log.Printf("Erro ao decodificar payload JSON para criar pedido: %v", err)
		http.Error(w, "Corpo da requisição inválido: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(reqPayload.Items) == 0 {
		http.Error(w, "O pedido deve conter pelo menos um item.", http.StatusBadRequest)
		return
	}
	if reqPayload.StudentID == "" { // VERIFICAÇÃO DO NOVO CAMPO OBRIGATÓRIO
		http.Error(w, "O ID do aluno (student_id) é obrigatório.", http.StatusBadRequest)
		return
	}
	// ... (validação dos itens do pedido como antes) ...
	for _, itemReq := range reqPayload.Items {
		if itemReq.MenuItemID == "" || itemReq.Quantity <= 0 {
			http.Error(w, "Cada item do pedido deve ter 'menu_item_id' e 'quantity' (>0) válida.", http.StatusBadRequest)
			return
		}
	}

	log.Printf("Usuário %s criando pedido para aluno %s com %d tipo(s) de item(ns).",
		userIDfromContext, reqPayload.StudentID, len(reqPayload.Items))

	// --- INÍCIO DA TRANSAÇÃO E LÓGICA ---
	tx, err := appDB.Begin()
	if err != nil { /* ... tratamento de erro ... */
		http.Error(w, "Erro servidor", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// ***** NOVA VALIDAÇÃO IMPORTANTE: Verificar se o student_id pertence ao parent_user_id (usuário logado) *****
	var studentOwnerCheckID string
	studentCheckQuery := "SELECT id FROM public.students WHERE id = $1 AND parent_user_id = $2"
	err = tx.QueryRow(studentCheckQuery, reqPayload.StudentID, userIDfromContext).Scan(&studentOwnerCheckID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("Validação falhou: Aluno ID %s não encontrado ou não pertence ao usuário %s.", reqPayload.StudentID, userIDfromContext)
			http.Error(w, "Aluno especificado inválido ou não pertence a este responsável.", http.StatusForbidden) // Ou 400 Bad Request
			return                                                                                                 // Rollback será chamado pelo defer
		}
		log.Printf("Erro ao validar aluno %s para usuário %s: %v", reqPayload.StudentID, userIDfromContext, err)
		http.Error(w, "Erro ao validar dados do pedido.", http.StatusInternalServerError)
		return // Rollback
	}
	// Se chegou aqui, o aluno pertence ao pai.

	var calculatedTotalAmount float64 = 0.0
	var itemsForOrder []OrderItem
	// ... (Lógica para validar itens do menu, calcular totalAmount, preparar itemsForOrder - SEM MUDANÇAS AQUI) ...
	// Esta parte deve continuar como estava, buscando preços, verificando disponibilidade, etc.
	for _, itemReq := range reqPayload.Items {
		var itemName string
		var itemPrice float64
		var itemIsAvailable bool
		menuItemQuery := "SELECT name, price, is_available FROM public.menu_items WHERE id = $1"
		// IMPORTANTE: Usar tx.QueryRow aqui dentro da transação
		errItem := tx.QueryRow(menuItemQuery, itemReq.MenuItemID).Scan(&itemName, &itemPrice, &itemIsAvailable)
		if errItem != nil { /* ... tratamento de erro de item não encontrado ... */
			http.Error(w, "Item menu não encontrado", http.StatusBadRequest)
			return
		}
		if !itemIsAvailable { /* ... tratamento de erro de item indisponível ... */
			http.Error(w, "Item indisponível", http.StatusBadRequest)
			return
		}
		calculatedTotalAmount += itemPrice * float64(itemReq.Quantity)
		itemsForOrder = append(itemsForOrder, OrderItem{
			// ID do OrderItemAPIResponse será preenchido após INSERT em order_items
			MenuItemID:      itemReq.MenuItemID,
			MenuItemName:    itemName, // Importante popular aqui se sua struct tem
			Quantity:        itemReq.Quantity,
			PriceAtPurchase: itemPrice,
		})
	}

	// Verificar créditos do usuário (pai/responsável)
	var userCredits float64
	// ... (Lógica para buscar userCredits do userIDfromContext - SEM MUDANÇAS AQUI) ...
	userCreditsQuery := "SELECT credits FROM public.users WHERE id = $1"
	errCredits := tx.QueryRow(userCreditsQuery, userIDfromContext).Scan(&userCredits)
	if errCredits != nil { /* ... tratamento de erro ... */
		http.Error(w, "Erro créditos", http.StatusInternalServerError)
		return
	}

	if userCredits < calculatedTotalAmount {
		// ... (Lógica de créditos insuficientes - SEM MUDANÇAS AQUI) ...
		http.Error(w, "Créditos insuficientes", http.StatusPaymentRequired)
		return
	}

	// Inserir na tabela 'orders' - AGORA INCLUINDO student_id
	var newOrder Order                  // Usando a struct Order que tem StudentID *string
	newOrder.UserID = userIDfromContext // ID do pai
	newOrder.TotalAmount = calculatedTotalAmount
	newOrder.Status = "PENDING"
	// Atribuir o student_id à struct newOrder para que seja retornado no JSON
	// O banco espera um UUID, então reqPayload.StudentID deve ser um UUID válido
	newOrder.StudentID = &reqPayload.StudentID

	// MODIFICADO: Adicionar student_id ao INSERT e ao RETURNING
	orderInsertQuery := `
		INSERT INTO public.orders (user_id, student_id, total_amount, status) 
		VALUES ($1, $2, $3, $4) 
		RETURNING id, order_date, created_at, updated_at, student_id;` // student_id também no RETURNING

	var returnedStudentID sql.NullString // Para scanear o student_id que pode ser nulo no DB, mas aqui estamos inserindo.
	// Se student_id na tabela orders é NOT NULL, não precisamos de sql.NullString.
	// Eu sugeri ADD COLUMN student_id UUID REFERENCES public.alunos(id) ON DELETE SET NULL;
	// Então, ele PODE ser NULL no banco, então sql.NullString é seguro.
	err = tx.QueryRow(orderInsertQuery, newOrder.UserID, reqPayload.StudentID, newOrder.TotalAmount, newOrder.Status).Scan(
		&newOrder.ID, &newOrder.OrderDate, &newOrder.CreatedAt, &newOrder.UpdatedAt, &returnedStudentID)
	if err != nil {
		log.Printf("Erro ao inserir pedido para usuário %s, aluno %s: %v", userIDfromContext, reqPayload.StudentID, err)
		http.Error(w, "Erro ao registrar o pedido.", http.StatusInternalServerError)
		return // Rollback
	}
	if returnedStudentID.Valid { // Atribuir de volta à struct de resposta
		newOrder.StudentID = &returnedStudentID.String
	}

	// Inserir na tabela 'order_items'
	// ... (Lógica para inserir order_items - SEM MUDANÇAS AQUI, mas referenciando newOrder.ID) ...
	// Certifique-se de que a struct OrderItemAPIResponse (ou a que você usa para itemsForOrder)
	// está correta e que você preenche os campos necessários.
	for i := range itemsForOrder {
		itemsForOrder[i].OrderID = newOrder.ID
		orderItemInsertQuery := `
			INSERT INTO public.order_items (order_id, menu_item_id, quantity, price_at_purchase) 
			VALUES ($1, $2, $3, $4) RETURNING id, created_at`
		// No Scan abaixo, a struct currentItem (dentro do loop de itemsForOrder) é que precisaria de MenuItemName.
		// A `itemsForOrder` já foi populada com MenuItemName. Agora só precisamos do ID e CreatedAt do BD.
		errItemInsert := tx.QueryRow(orderItemInsertQuery, itemsForOrder[i].OrderID, itemsForOrder[i].MenuItemID, itemsForOrder[i].Quantity, itemsForOrder[i].PriceAtPurchase).Scan(
			&itemsForOrder[i].ID, &itemsForOrder[i].CreatedAt) // Assume que sua struct OrderItemAPIResponse tem ID e CreatedAt
		if errItemInsert != nil { /* ... tratamento de erro ... */
			http.Error(w, "Erro itens pedido", http.StatusInternalServerError)
			return
		}
	}
	newOrder.Items = itemsForOrder

	// Atualizar créditos do usuário (pai)
	// ... (Lógica para atualizar créditos - SEM MUDANÇAS AQUI) ...
	updateCreditsQuery := "UPDATE public.users SET credits = credits - $1 WHERE id = $2"
	_, errUpdateCredits := tx.Exec(updateCreditsQuery, newOrder.TotalAmount, userIDfromContext)
	if errUpdateCredits != nil { /* ... tratamento de erro ... */
		http.Error(w, "Erro atualizar créditos", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil { /* ... tratamento de erro ... */
		http.Error(w, "Erro commit", http.StatusInternalServerError)
		return
	}

	log.Printf("Pedido %s criado com sucesso para usuário %s, aluno %s.", newOrder.ID, userIDfromContext, reqPayload.StudentID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newOrder)
}

// handleGetMyOrders busca e retorna o histórico de pedidos do usuário autenticado
func handleGetMyOrders(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	userIDfromContext := r.Context().Value(userContextKey)
	if userIDfromContext == nil {
		http.Error(w, "Usuário não autenticado", http.StatusUnauthorized)
		return
	}
	userID, ok := userIDfromContext.(string)
	if !ok || userID == "" {
		http.Error(w, "ID de usuário inválido no token", http.StatusUnauthorized)
		return
	}

	log.Printf("Buscando pedidos para o usuário ID: %s", userID)

	ordersQuery := `
		SELECT id, user_id, order_date, total_amount, status, created_at, updated_at 
		FROM public.orders 
		WHERE user_id = $1 
		ORDER BY order_date DESC;`

	rows, err := appDB.Query(ordersQuery, userID)
	if err != nil {
		log.Printf("Erro ao buscar pedidos para usuário %s: %v", userID, err)
		http.Error(w, "Erro ao buscar histórico de pedidos.", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var userOrders []Order

	for rows.Next() {
		var order Order
		errScanOrder := rows.Scan(
			&order.ID,
			&order.UserID,
			&order.OrderDate,
			&order.TotalAmount,
			&order.Status,
			&order.CreatedAt,
			&order.UpdatedAt,
		)
		if errScanOrder != nil {
			log.Printf("Erro ao scanear linha do pedido para usuário %s: %v", userID, errScanOrder)
			http.Error(w, "Erro ao processar histórico de pedidos.", http.StatusInternalServerError)
			return
		}

		// Buscar e popular os itens do pedido
		orderItemsDetails, errItems := fetchOrderItemsByOrderID(appDB, order.ID)
		if errItems != nil {
			log.Printf("Alerta: Não foi possível buscar itens para o pedido %s: %v", order.ID, errItems)
			order.Items = []OrderItem{} // Garante que Items não seja nulo no JSON de resposta
		} else {
			order.Items = orderItemsDetails
		}

		userOrders = append(userOrders, order)
	}
	if err = rows.Err(); err != nil {
		log.Printf("Erro após iterar pelos pedidos do usuário %s: %v", userID, err)
		http.Error(w, "Erro ao processar dados dos pedidos.", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userOrders)
}

// handleAdminGetOrders lista pedidos para admin/staff, com filtro opcional por status
func handleAdminGetOrders(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	userIDfromContext := r.Context().Value(userContextKey)
	if userIDfromContext == nil {
		http.Error(w, "Usuário não autenticado", http.StatusUnauthorized)
		return
	}
	requestingUserID := userIDfromContext.(string)

	requestingUserProfile, err := fetchUserProfile(requestingUserID, appDB)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("Perfil não encontrado para usuário solicitante: %s", requestingUserID)
			http.Error(w, "Usuário solicitante não encontrado ou perfil não configurado.", http.StatusUnauthorized)
		} else {
			log.Printf("Erro ao buscar perfil do usuário solicitante %s: %v", requestingUserID, err)
			http.Error(w, "Erro no servidor ao verificar permissões.", http.StatusInternalServerError)
		}
		return
	}

	isAllowed := false
	isPrivilegedViewer := false

	// Usando os papéis que você definiu: CLIENT, STAFF, ADMIN, SUPER_ADMIN
	if requestingUserProfile.Role == "admin" || requestingUserProfile.Role == "super_admin" {
		isAllowed = true
		isPrivilegedViewer = true
	} else if requestingUserProfile.Role == "staff" {
		isAllowed = true
	}

	if !isAllowed {
		log.Printf("Usuário %s (Papel: %s) tentou acessar GET /orders sem permissão.", requestingUserID, requestingUserProfile.Role)
		http.Error(w, "Acesso não autorizado para este recurso.", http.StatusForbidden)
		return
	}

	log.Printf("Usuário %s (Papel: %s) acessando GET /orders", requestingUserID, requestingUserProfile.Role)

	statusFilter := r.URL.Query().Get("status")
	baseQuery := "SELECT id, user_id, order_date, total_amount, status, created_at, updated_at FROM public.orders"
	var queryParams []interface{}
	conditions := []string{}
	paramCounter := 1

	if isPrivilegedViewer {
		if statusFilter != "" {
			conditions = append(conditions, fmt.Sprintf("status = $%d", paramCounter))
			queryParams = append(queryParams, statusFilter)
			paramCounter++
		}
	} else if requestingUserProfile.Role == "staff" {
		if statusFilter != "" {
			if statusFilter == "PENDING" || statusFilter == "PREPARING" || statusFilter == "READY" {
				conditions = append(conditions, fmt.Sprintf("status = $%d", paramCounter))
				queryParams = append(queryParams, statusFilter)
				paramCounter++
			} else {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]Order{})
				return
			}
		} else {
			conditions = append(conditions, fmt.Sprintf("status IN ($%d, $%d, $%d)", paramCounter, paramCounter+1, paramCounter+2))
			queryParams = append(queryParams, "PENDING", "PREPARING", "READY")
			paramCounter += 3
		}
	}

	ordersQueryString := baseQuery
	if len(conditions) > 0 {
		ordersQueryString += " WHERE " + strings.Join(conditions, " AND ")
	}
	ordersQueryString += " ORDER BY order_date DESC;"

	log.Printf("Executando query para /orders: %s com params: %v", ordersQueryString, queryParams)

	orderRows, err := appDB.Query(ordersQueryString, queryParams...) // Renomeado para orderRows para evitar conflito
	if err != nil {
		log.Printf("Erro ao buscar todos os pedidos (admin/staff): %v", err)
		http.Error(w, "Erro ao buscar lista de pedidos.", http.StatusInternalServerError)
		return
	}
	defer orderRows.Close()

	var allOrders []Order
	for orderRows.Next() {
		var order Order
		errScan := orderRows.Scan(&order.ID, &order.UserID, &order.OrderDate, &order.TotalAmount, &order.Status, &order.CreatedAt, &order.UpdatedAt)
		if errScan != nil {
			log.Printf("Erro ao scanear pedido (admin/staff): %v", errScan)
			continue
		}

		orderItemsDetails, errItems := fetchOrderItemsByOrderID(appDB, order.ID)
		if errItems != nil {
			log.Printf("Alerta: Não foi possível buscar itens para o pedido %s (admin view): %v", order.ID, errItems)
			order.Items = []OrderItem{}
		} else {
			order.Items = orderItemsDetails
		}

		allOrders = append(allOrders, order)
	}
	if err = orderRows.Err(); err != nil {
		log.Printf("Erro após iterar pelos pedidos (admin/staff): %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allOrders)
}

// NOVO: ordersRouterHandler para lidar com rotas /orders/ e /orders/{id}
func ordersRouterHandler(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	path := r.URL.Path
	orderIDSegment := strings.TrimPrefix(path, "/orders/")
	orderIDSegment = strings.Trim(orderIDSegment, "/")

	log.Printf("DEBUG: ordersRouterHandler: Path: %s, orderIDSegment: '%s', Method: %s", path, orderIDSegment, r.Method)

	if orderIDSegment == "" { // Rota base: /orders/
		// ... (código para GET /orders/ e POST /orders/ continua o mesmo) ...
		switch r.Method {
		case http.MethodGet: // Listar pedidos (admin/staff)
			authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				handleAdminGetOrders(ww, rr, appDB)
			})).ServeHTTP(w, r)
		case http.MethodPost: // Criar pedido
			authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				handleCreateOrder(ww, rr, appDB)
			})).ServeHTTP(w, r)
		default:
			http.Error(w, "Método não permitido para /orders/", http.StatusMethodNotAllowed)
		}
	} else { // Rota com ID: /orders/{id}
		orderID := orderIDSegment
		switch r.Method {
		case http.MethodGet: // <<< --- NOVA LÓGICA PARA GET /{id}
			authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				handleGetOrderByID(ww, rr, appDB, orderID) // Passa o orderID extraído
			})).ServeHTTP(w, r)
		case http.MethodPut: // Atualizar status
			authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				handleUpdateOrderStatus(ww, rr, appDB, orderID) // Passa o orderID extraído
			})).ServeHTTP(w, r)
		// case http.MethodDelete: // Futuramente para deletar/cancelar um pedido por ID
		// 	log.Printf("Rota DELETE /orders/%s chamada (ainda não implementada)", orderID)
		// 	http.Error(w, fmt.Sprintf("DELETE para /orders/%s ainda não implementado", orderID), http.StatusNotImplemented)
		default:
			http.Error(w, fmt.Sprintf("Método não permitido para /orders/%s", orderID), http.StatusMethodNotAllowed)
		}
	}
}

// NOVO: fetchOrderItemsByOrderID busca todos os itens para um determinado ID de pedido
func fetchOrderItemsByOrderID(appDB *sql.DB, orderID string) ([]OrderItem, error) {
	itemsQuery := `
		        SELECT 
            oi.id, 
            oi.order_id, 
            oi.menu_item_id, 
            mi.name AS menu_item_name, -- Buscando o nome da tabela menu_items
            oi.quantity, 
            oi.price_at_purchase, 
            oi.created_at 
        FROM public.order_items oi
        JOIN public.menu_items mi ON oi.menu_item_id = mi.id -- JOIN com menu_items
        WHERE oi.order_id = $1;`

	// Usar uma nova variável para as linhas dos itens, não a 'rows' dos pedidos
	itemRows, err := appDB.Query(itemsQuery, orderID) // <<-- Correto: itemRows
	if err != nil {
		if err == sql.ErrNoRows {
			return []OrderItem{}, nil
		}
		return nil, fmt.Errorf("erro ao buscar itens para o pedido %s: %w", orderID, err)
	}
	defer itemRows.Close() // <<-- Correto: itemRows.Close()

	var orderItems []OrderItem
	for itemRows.Next() { // <<-- Correto: itemRows.Next()
		var item OrderItem
		errScan := itemRows.Scan( // <<-- Correto: itemRows.Scan()
			&item.ID,
			&item.OrderID,
			&item.MenuItemID,
			&item.MenuItemName,
			&item.Quantity,
			&item.PriceAtPurchase,
			&item.CreatedAt,
		)
		if errScan != nil {
			return nil, fmt.Errorf("erro ao scanear item do pedido %s: %w", orderID, errScan)
		}
		orderItems = append(orderItems, item)
	}
	if err = itemRows.Err(); err != nil { // <<-- Correto: itemRows.Err()
		return nil, fmt.Errorf("erro após iterar pelos itens do pedido %s: %w", orderID, err)
	}
	return orderItems, nil
}

// handleGetOrderByID busca um pedido específico pelo seu ID, com verificação de permissão
func handleGetOrderByID(w http.ResponseWriter, r *http.Request, appDB *sql.DB, orderIDFromPath string) {
	// 1. Autenticação já foi feita. Pegar userID e perfil (para checar o papel).
	userIDfromContext := r.Context().Value(userContextKey)
	if userIDfromContext == nil {
		http.Error(w, "Usuário não autenticado", http.StatusUnauthorized)
		return
	}
	requestingUserID := userIDfromContext.(string)

	requestingUserProfile, err := fetchUserProfile(requestingUserID, appDB) // Usando a função auxiliar
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Perfil de usuário solicitante não encontrado.", http.StatusUnauthorized)
		} else {
			log.Printf("Erro ao buscar perfil do usuário %s: %v", requestingUserID, err)
			http.Error(w, "Erro no servidor ao verificar permissões.", http.StatusInternalServerError)
		}
		return
	}

	log.Printf("Usuário %s (Papel: %s) tentando buscar pedido com ID: %s", requestingUserID, requestingUserProfile.Role, orderIDFromPath)

	// 2. Buscar o pedido pelo ID fornecido na URL
	var order Order
	orderQuery := `
		SELECT id, user_id, order_date, total_amount, status, created_at, updated_at 
		FROM public.orders 
		WHERE id = $1;`

	err = appDB.QueryRow(orderQuery, orderIDFromPath).Scan(
		&order.ID,
		&order.UserID,
		&order.OrderDate,
		&order.TotalAmount,
		&order.Status,
		&order.CreatedAt,
		&order.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Pedido não encontrado.", http.StatusNotFound)
		} else {
			log.Printf("Erro ao buscar pedido por ID (%s): %v", orderIDFromPath, err)
			http.Error(w, "Erro no servidor ao buscar pedido.", http.StatusInternalServerError)
		}
		return
	}

	// 3. Autorização: Verificar se o usuário pode ver este pedido específico
	canViewOrder := false
	if requestingUserProfile.Role == "ADMIN" || requestingUserProfile.Role == "SUPER_ADMIN" || requestingUserProfile.Role == "STAFF" {
		canViewOrder = true
	} else if requestingUserProfile.Role == "CLIENTE" && order.UserID == requestingUserID {
		canViewOrder = true
	}

	if !canViewOrder {
		log.Printf("Usuário %s (Papel: %s) não autorizado a ver o pedido %s (pertence ao usuário %s).",
			requestingUserID, requestingUserProfile.Role, orderIDFromPath, order.UserID)
		http.Error(w, "Acesso não autorizado a este pedido.", http.StatusForbidden)
		return
	}

	// 4. Buscar os itens do pedido
	orderItems, errItems := fetchOrderItemsByOrderID(appDB, order.ID)
	if errItems != nil {
		log.Printf("Alerta: Não foi possível buscar itens para o pedido %s: %v", order.ID, errItems)
		order.Items = []OrderItem{} // Retorna com itens vazios se houver erro aqui
	} else {
		order.Items = orderItems
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(order)
}
