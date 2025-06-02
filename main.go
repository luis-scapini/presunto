package main // Continua sendo o pacote principal

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	// Não precisamos mais de "encoding/json" ou "time" aqui, pois foram para menu_handlers.go
	_ "github.com/lib/pq" // Driver PostgreSQL, importado pelos seus efeitos colaterais
)

var db *sql.DB // Variável global para a conexão com o banco de dados

func main() {
	err := initDB()
	if err != nil {
		log.Fatalf("Erro ao inicializar conexão com o banco de dados: %v", err)
	}
	defer db.Close() // Garante que a conexão seja fechada quando a função main terminar

	err = db.Ping()
	if err != nil {
		log.Printf("Alerta: Erro ao fazer ping no banco de dados: %v.", err)
	} else {
		log.Println("Conexão com o banco de dados PostgreSQL estabelecida com sucesso!")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	// Rota principal (Olá Mundo)
	http.HandleFunc("/", rootHandler)

	// Rota para /menu-items agora chama o handler passando a conexão 'db'
	http.HandleFunc("/menu-items/", func(w http.ResponseWriter, r *http.Request) {
		menuItemsRouterHandler(w, r, db) // Passamos 'db' para o handler
	})

	// NOVA ROTA: Obter perfil do usuário logado (protegida)
	http.HandleFunc("/me/profile", func(w http.ResponseWriter, r *http.Request) {
		// Primeiro, passa pelo middleware de autenticação
		authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
			// Se autenticado, chama o handler para buscar o perfil
			handleGetMyProfile(ww, rr, db)
		})).ServeHTTP(w, r) // Importante: ServeHTTP(w,r) original da requisição
	})

	// NOVO: Rota para pedidos
	http.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		// Roteador simples baseado no método HTTP para /orders
		// Por enquanto, só POST para criar. GET virá depois.
		if r.Method == http.MethodPost {
			authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				handleCreateOrder(ww, rr, db) // Chama o handler do order_handlers.go
			})).ServeHTTP(w, r)
		} else {
			http.Error(w, "Método não permitido para /orders. Use POST para criar.", http.StatusMethodNotAllowed)
		}
	})

	// NOVA ROTA: Listar os pedidos do usuário logado (protegida)
	http.HandleFunc("/me/orders", func(w http.ResponseWriter, r *http.Request) {
		// Apenas o método GET é permitido por enquanto para esta rota
		if r.Method == http.MethodGet {
			authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				handleGetMyOrders(ww, rr, db) // Chama o handler do order_handlers.go
			})).ServeHTTP(w, r)
		} else {
			http.Error(w, "Método não permitido para /me/orders. Use GET.", http.StatusMethodNotAllowed)
		}
	})

	// ATUALIZADO: Rota para pedidos agora usa o ordersRouterHandler
	http.HandleFunc("/orders/", func(w http.ResponseWriter, r *http.Request) { // Mantenha a barra no final
		ordersRouterHandler(w, r, db) // Chama o roteador de pedidos do order_handlers.go
	})

	// NOVA ROTA: Para Turmas (Classes)
	http.HandleFunc("/classes/", func(w http.ResponseWriter, r *http.Request) {
		classRouterHandler(w, r, db) // Chama o roteador de turmas
	})

	// NOVA ROTA: Para Alunos (Students)
	http.HandleFunc("/students/", func(w http.ResponseWriter, r *http.Request) { // Note a barra no final
		studentRouterHandler(w, r, db)
	})
	http.HandleFunc("/me/students", func(w http.ResponseWriter, r *http.Request) { // Rota específica para "meus alunos"
		studentRouterHandler(w, r, db) // O studentRouterHandler vai tratar o path /me/students
	})

	log.Printf("Servidor escutando na porta %s", port)
	// MODIFICADO: Adicionamos o corsMiddleware para envolver todos os handlers
	if err := http.ListenAndServe(":"+port, corsMiddleware(http.DefaultServeMux)); err != nil {
		log.Fatalf("Erro ao iniciar servidor HTTP: %v", err)
	}
}

// initDB inicializa a conexão com o banco de dados
func initDB() error {
	// Pegar as variáveis de ambiente para a conexão com o banco
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	dbSSLMode := os.Getenv("DB_SSLMODE")

	if dbHost == "" || dbPort == "" || dbUser == "" || dbPassword == "" || dbName == "" {
		log.Println("Atenção: Variáveis de ambiente do banco de dados não estão todas configuradas.")
		return fmt.Errorf("variáveis de ambiente do banco de dados incompletas")
	}
	if dbSSLMode == "" {
		dbSSLMode = "require" // Supabase geralmente requer SSL
	}

	connStr := fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=%s",
		dbUser, dbPassword, dbHost, dbPort, dbName, dbSSLMode)

	var err_db_open error
	db, err_db_open = sql.Open("postgres", connStr)
	if err_db_open != nil {
		return fmt.Errorf("erro ao abrir conexão com o banco: %w", err_db_open)
	}
	return nil
}

// rootHandler para a rota principal "/"
func rootHandler(w http.ResponseWriter, r *http.Request) {
	dbStatus := "desconectado"
	if db != nil { // Verifica se db foi inicializado
		pingErr := db.Ping() // Faz um novo ping para status atualizado
		if pingErr == nil {
			dbStatus = "conectado com sucesso!"
		} else {
			dbStatus = fmt.Sprintf("erro ao conectar: %v", pingErr)
		}
	}
	fmt.Fprintf(w, "Olá, Cantina do Super ERP! Backend em Go no ar!\nStatus do Banco de Dados: %s", dbStatus)
}

// corsMiddleware adiciona os cabeçalhos CORS necessários
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Define quais origens são permitidas. "*" significa qualquer origem.
		// Para produção, você restringiria isso ao domínio do seu frontend Flutter.
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Define quais métodos HTTP são permitidos
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

		// Define quais cabeçalhos HTTP podem ser usados na requisição real
		// É importante incluir "Authorization" (para o token JWT) e "Content-Type".
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")

		// w.Header().Set("Access-Control-Allow-Credentials", "true") // Descomente se precisar de cookies/sessões autenticadas

		// Se a requisição for um OPTIONS (preflight request), apenas retorne os cabeçalhos.
		// O navegador envia uma requisição OPTIONS antes de algumas requisições POST/PUT/DELETE
		// ou requisições com certos cabeçalhos para verificar as permissões CORS.
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK) // Ou http.StatusNoContent
			return
		}

		// Chama o próximo handler na cadeia (seu roteador principal)
		next.ServeHTTP(w, r)
	})
}
