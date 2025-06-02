// astera/services/cantina-service/student_handlers.go
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lib/pq"
	// uuid "github.com/google/uuid" // Se for gerar UUID no Go
)

// Struct Student (como definida antes, coloque aqui ou importe de um arquivo de modelos)
// Poderia ir em um arquivo como models_educational.go ou student_handlers.go
type Student struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	ClassID      string    `json:"class_id"`               // UUID da turma
	ParentUserID string    `json:"parent_user_id"`         // UUID do pai/responsável (da tabela users)
	ClassName    *string   `json:"class_name,omitempty"`   // Para retornar o nome da turma (via JOIN)
	ParentEmail  *string   `json:"parent_email,omitempty"` // Para retornar o email do pai (via JOIN)
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Payload para criar um aluno
type CreateStudentPayload struct {
	Name    string `json:"name"`
	ClassID string `json:"class_id"` // UUID da turma
	// ParentUserID será pego do token para CLIENTE, ou pode ser opcional no payload para ADMIN
	ParentUserID *string `json:"parent_user_id,omitempty"`
}

// studentRouterHandler para /students e /students/{id} e /me/students
func studentRouterHandler(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	path := r.URL.Path
	log.Printf("DEBUG: studentRouterHandler: Path: %s, Method: %s", path, r.Method)

	// Rota para /me/students
	if strings.HasPrefix(path, "/me/students") {
		// Este if precisa ser ajustado se /me/students tiver sub-rotas ou IDs
		// Por agora, vamos assumir que /me/students é apenas para listar os alunos do usuário logado
		if r.Method == http.MethodGet {
			authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				handleGetMyStudents(ww, rr, appDB)
			})).ServeHTTP(w, r)
		} else {
			http.Error(w, "Método não permitido para /me/students", http.StatusMethodNotAllowed)
		}
		return
	}

	// Rotas para /students/ e /students/{id}
	idSegment := strings.TrimPrefix(path, "/students/")
	idSegment = strings.Trim(idSegment, "/")

	if idSegment == "" { // Rota base: /students/
		switch r.Method {
		case http.MethodPost:
			authMiddleware(http.HandlerFunc(func(ww http.ResponseWriter, rr *http.Request) {
				handleCreateStudent(ww, rr, appDB)
			})).ServeHTTP(w, r)
		// case http.MethodGet:
		// TODO: handleGetAllStudents (para ADMIN/SUPER_ADMIN)
		default:
			http.Error(w, "Método não permitido para /students/", http.StatusMethodNotAllowed)
		}
	} else { // Rota com ID: /students/{id}
		studentID := idSegment
		switch r.Method {
		// case http.MethodGet:
		// TODO: handleGetStudentByID
		// case http.MethodPut:
		// TODO: handleUpdateStudent
		// case http.MethodDelete:
		// TODO: handleDeleteStudent
		default:
			http.Error(w, fmt.Sprintf("Método para /students/%s não implementado ou não permitido", studentID), http.StatusMethodNotAllowed)
		}
	}
}

func handleCreateStudent(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	userIDfromContext := r.Context().Value(userContextKey).(string)
	requestingUserProfile, err := fetchUserProfile(userIDfromContext, appDB)
	if err != nil {
		http.Error(w, "Erro ao verificar permissões.", http.StatusInternalServerError)
		return
	}

	// MODIFICADO: Apenas ADMIN ou SUPER_ADMIN podem criar alunos
	if requestingUserProfile.Role != "admin" && requestingUserProfile.Role != "super_admin" {
		http.Error(w, "Acesso não autorizado para criar alunos.", http.StatusForbidden)
		return
	}

	var payload CreateStudentPayload // CreateStudentPayload deve ter: Name, ClassID, ParentUserID (obrigatório para Admin)
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Payload inválido: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// MODIFICADO: Admin DEVE fornecer Name, ClassID e ParentUserID
	if strings.TrimSpace(payload.Name) == "" ||
		strings.TrimSpace(payload.ClassID) == "" ||
		payload.ParentUserID == nil || // Verifica se o ponteiro é nulo
		strings.TrimSpace(*payload.ParentUserID) == "" { // Verifica se o valor do ponteiro é vazio
		http.Error(w, "Nome do aluno, ID da turma e ID do pai/responsável são obrigatórios.", http.StatusBadRequest)
		return
	}

	var parentIDToUse string
	if requestingUserProfile.Role == "admin" || requestingUserProfile.Role == "super_admin" {
		// Admin pode especificar o parent_user_id no payload
		if payload.ParentUserID == nil || *payload.ParentUserID == "" {
			http.Error(w, "Admin deve especificar o parent_user_id para o novo aluno.", http.StatusBadRequest)
			return
		}
		parentIDToUse = *payload.ParentUserID
		// TODO: Validar se este parentIDToUse existe na tabela users.
	} else if requestingUserProfile.Role == "CLIENTE" {
		// Cliente (pai) só pode criar aluno vinculado a si mesmo. Ignora payload.ParentUserID.
		parentIDToUse = requestingUserProfile.ID
	} else {
		http.Error(w, "Acesso não autorizado para criar alunos.", http.StatusForbidden)
		return
	}

	var newStudent Student
	sqlStatement := `
		INSERT INTO public.students (name, class_id, parent_user_id) VALUES ($1, $2, $3)
		RETURNING id, name, class_id, parent_user_id, created_at, updated_at`

	// Para o scan, precisamos lidar com ClassName e ParentEmail que não vêm direto do INSERT simples
	// Vamos retornar o que temos e o frontend pode buscar detalhes se necessário, ou fazemos JOINs depois.
	// Por agora, vamos simplificar o RETURNING e o Scan.
	// Se quisermos retornar ClassName e ParentEmail, precisaremos de um SELECT após o INSERT ou um JOIN complexo.
	// Para este INSERT, o mais simples é retornar apenas os campos diretos da tabela students.
	// A struct Student precisaria ser ajustada para o Scan ou usamos uma struct temporária.

	// Vamos popular os campos que temos diretamente:
	newStudent.Name = payload.Name
	newStudent.ClassID = payload.ClassID
	newStudent.ParentUserID = parentIDToUse

	// Executa o INSERT e pega o ID gerado e timestamps
	err = appDB.QueryRow(sqlStatement, payload.Name, payload.ClassID, *payload.ParentUserID).Scan(
		&newStudent.ID,
		&newStudent.Name,
		&newStudent.ClassID,
		&newStudent.ParentUserID,
		&newStudent.CreatedAt,
		&newStudent.UpdatedAt,
	)

	if err != nil {
		// Aqui, verificar se o erro é de chave estrangeira (FK) pode ser útil.
		// Ex: Se class_id não existe, ou parent_user_id não existe.
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code.Name() == "foreign_key_violation" {
				if strings.Contains(pqErr.Constraint, "students_class_id_fkey") {
					http.Error(w, "ID da Turma fornecido não existe.", http.StatusBadRequest)
					return
				}
				if strings.Contains(pqErr.Constraint, "students_parent_user_id_fkey") {
					http.Error(w, "ID do Pai/Responsável fornecido não existe ou não é válido.", http.StatusBadRequest)
					return
				}
			}
		}
		log.Printf("Erro ao inserir aluno no banco: %v", err)
		http.Error(w, "Erro ao criar aluno.", http.StatusInternalServerError)
		return
	}

	// Omitindo ClassName e ParentEmail na resposta do POST para simplificar por enquanto
	newStudent.ClassName = nil
	newStudent.ParentEmail = nil

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newStudent)
}

// handleGetMyStudents lista os alunos vinculados ao usuário (pai/responsável) logado
func handleGetMyStudents(w http.ResponseWriter, r *http.Request, appDB *sql.DB) {
	userIDfromContext := r.Context().Value(userContextKey).(string) // AuthMiddleware já validou
	requestingUserProfile, err := fetchUserProfile(userIDfromContext, appDB)
	if err != nil { /* ... tratamento de erro ... */
		http.Error(w, "Erro permissões", http.StatusInternalServerError)
		return
	}

	// Apenas CLIENTEs podem ter "seus" alunos neste contexto.
	// Admins/Staff usariam GET /students para ver todos ou filtrar.
	if requestingUserProfile.Role != "CLIENTE" {
		http.Error(w, "Esta rota é apenas para usuários do tipo CLIENTE.", http.StatusForbidden)
		return
	}

	log.Printf("Buscando alunos para o CLIENTE ID: %s", userIDfromContext)

	var students []Student // Slice para armazenar os alunos

	// Query para buscar alunos do pai, incluindo o nome da turma
	query := `
		SELECT 
			s.id, s.name, s.class_id, c.name AS class_name, 
			s.parent_user_id, s.created_at, s.updated_at
		FROM public.students s
		LEFT JOIN public.classes c ON s.class_id = c.id
		WHERE s.parent_user_id = $1
		ORDER BY s.name ASC;`
	// LEFT JOIN para o caso de um aluno estar temporariamente sem turma (class_id NULL)

	rows, err := appDB.Query(query, userIDfromContext)
	if err != nil {
		log.Printf("Erro ao buscar alunos para o pai %s: %v", userIDfromContext, err)
		http.Error(w, "Erro ao buscar lista de alunos.", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var student Student
		var className sql.NullString // Para lidar com class_name que pode ser NULL (se class_id for NULL)

		errScan := rows.Scan(
			&student.ID,
			&student.Name,
			&student.ClassID, // Mesmo que class_id possa ser NULL no DB, nossa struct Student espera string. Ajustar se necessário.
			// Se class_id for NULLABLE no DB e na struct Student for string (não *string), Scan falhará.
			// Para ser seguro, class_id na struct Student deveria ser *string ou sql.NullString no Scan.
			// Vamos assumir que class_id é obrigatório ao criar aluno por enquanto.
			&className,
			&student.ParentUserID,
			&student.CreatedAt,
			&student.UpdatedAt,
		)
		if errScan != nil {
			log.Printf("Erro ao scanear aluno para o pai %s: %v", userIDfromContext, errScan)
			// Considerar continuar para o próximo aluno em vez de retornar erro 500 geral
			continue
		}
		if className.Valid {
			student.ClassName = &className.String
		}
		// ParentEmail não está nesta query, então será nulo na resposta
		students = append(students, student)
	}
	if err = rows.Err(); err != nil {
		log.Printf("Erro após iterar pelos alunos do pai %s: %v", userIDfromContext, err)
		// Não envie http.Error aqui se já pode ter enviado parte da resposta ou se for erro de iteração apenas
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(students)
}
