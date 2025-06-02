# Etapa 1: Build da aplicação Go
# Usamos uma imagem oficial do Go como base para compilar nosso código.
# Alinhei com a versão do seu go.mod, mas 1.22-alpine também funcionaria.
FROM golang:1.21-alpine AS builder

# Define o diretório de trabalho dentro do container
WORKDIR /app

# Copia PRIMEIRO os arquivos de módulo.
# Se go.sum não existir no seu contexto local, não haverá problema aqui ainda.
COPY go.mod ./
# Se go.sum existir localmente, copie. Se não, esta linha pode ser omitida ou
# tratada de outra forma, mas vamos tentar executar o tidy dentro do container.
# COPY go.sum ./ 

# Copia todo o código fonte restante.
# É importante copiar o código ANTES de rodar go mod tidy e go mod download
# para que esses comandos possam analisar as importações no seu código.
COPY *.go ./

# Executa o go mod tidy DENTRO do container.
# Isso deve gerar um go.sum se for necessário, baseado no go.mod e nos arquivos .go copiados.
RUN go mod tidy

# Baixa as dependências (se houver alguma externa, o tidy já deve ter cuidado disso)
RUN go mod download

# Compila a aplicação Go.
RUN CGO_ENABLED=0 GOOS=linux go build -v -o /app/server .

# Etapa 2: Imagem final, otimizada e pequena
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server /app/server
ENTRYPOINT ["/app/server"]