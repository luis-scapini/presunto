package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Chave para usar no contexto da requisição para armazenar o ID do usuário
type contextKey string

const userContextKey = contextKey("userID")

// authMiddleware verifica o token JWT do Supabase
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwtSecret := os.Getenv("SUPABASE_JWT_SECRET")
		if jwtSecret == "" {
			log.Println("ERRO FATAL: SUPABASE_JWT_SECRET não está configurado no ambiente.")
			http.Error(w, "Configuração do servidor incompleta", http.StatusInternalServerError)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Cabeçalho de autorização ausente", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			http.Error(w, "Formato do cabeçalho de autorização inválido (esperado: Bearer <token>)", http.StatusUnauthorized)
			return
		}
		tokenString := parts[1]

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			// Verifica o método de assinatura
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("método de assinatura inesperado: %v", token.Header["alg"])
			}
			return []byte(jwtSecret), nil
		})

		if err != nil {
			log.Printf("Erro ao parsear/validar token: %v", err)
			http.Error(w, "Token inválido ou expirado", http.StatusUnauthorized)
			return
		}

		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			// Token é válido. Podemos extrair o ID do usuário (geralmente na claim 'sub')
			userID, ok := claims["sub"].(string)
			if !ok || userID == "" {
				log.Println("Erro: Claim 'sub' (userID) não encontrada ou inválida no token.")
				http.Error(w, "Token inválido (sem ID de usuário)", http.StatusUnauthorized)
				return
			}

			// Adiciona o userID ao contexto da requisição para que os handlers possam usá-lo
			ctx := context.WithValue(r.Context(), userContextKey, userID)
			log.Printf("Usuário autenticado: %s", userID)
			next.ServeHTTP(w, r.WithContext(ctx)) // Prossegue para o próximo handler com o contexto atualizado
		} else {
			log.Printf("Token JWT inválido ou claims não são MapClaims. Claims: %+v, Válido: %v", token.Claims, token.Valid)
			http.Error(w, "Token inválido", http.StatusUnauthorized)
		}
	})
}
