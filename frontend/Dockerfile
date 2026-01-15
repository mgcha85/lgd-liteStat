# Stage 1: Development dependencies and build
FROM node:20-alpine AS builder

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install all dependencies
RUN npm install

# Copy source code
COPY . .

# Build for production
RUN npm run build

# Stage 2: Production with nginx
FROM nginx:alpine

# Install getent for DNS resolution
RUN apk add --no-cache bind-tools

# Copy built files from builder
COPY --from=builder /app/dist /usr/share/nginx/html

# Copy startup script
COPY run.sh /run.sh
RUN chmod +x /run.sh

# Expose port 80
EXPOSE 80

# Override BOTH entrypoint and cmd to use only our script
ENTRYPOINT []
CMD ["/bin/sh", "/run.sh"]
