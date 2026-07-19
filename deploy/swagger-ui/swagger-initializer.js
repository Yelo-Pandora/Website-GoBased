window.addEventListener('load', () => {
  window.ui = SwaggerUIBundle({
    deepLinking: true,
    displayRequestDuration: true,
    docExpansion: 'list',
    dom_id: '#swagger-ui',
    filter: true,
    persistAuthorization: true,
    tryItOutEnabled: true,
    url: '/platform-api.openapi.yaml',
    withCredentials: true,
  });
});
