# ---- build ----
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json ./
RUN npm install
COPY . .
RUN npm run build

# ---- serve ----
FROM nginx:1.27-alpine
LABEL org.opencontainers.image.source="https://github.com/happymooguild/wardn" \
      org.opencontainers.image.title="wardn-frontend" \
      org.opencontainers.image.description="wardn dashboard (SPA) served by nginx, proxying /api to the backend"
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
