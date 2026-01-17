#!/bin/bash

# This script adds spaceContext parameter to service method calls in handlers

echo "Fixing document handler methods..."

# Fix GetDocumentByID calls
sed -i 's/h\.documentService\.GetDocumentByID(c\.Request\.Context(), documentID, userID)/spaceContext, err := middleware.GetSpaceContext(c); if err != nil { c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required")); return }; document, err := h.documentService.GetDocumentByID(c.Request.Context(), documentID, userID, spaceContext)/g' internal/handlers/document.go

# Fix UpdateDocument calls
sed -i 's/h\.documentService\.UpdateDocument(c\.Request\.Context(), documentID, req, userID)/spaceContext, err := middleware.GetSpaceContext(c); if err != nil { c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required")); return }; document, err := h.documentService.UpdateDocument(c.Request.Context(), documentID, req, userID, spaceContext)/g' internal/handlers/document.go

# Fix DeleteDocument calls  
sed -i 's/h\.documentService\.DeleteDocument(c\.Request\.Context(), documentID, userID)/spaceContext, err := middleware.GetSpaceContext(c); if err != nil { c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required")); return }; err = h.documentService.DeleteDocument(c.Request.Context(), documentID, userID, spaceContext)/g' internal/handlers/document.go

echo "Fixing notebook handler methods..."

# Fix notebook CreateNotebook calls
sed -i 's/h\.notebookService\.CreateNotebook(c\.Request\.Context(), req, userID)/spaceContext, err := middleware.GetSpaceContext(c); if err != nil { c.JSON(http.StatusBadRequest, errors.BadRequest("Space context is required")); return }; notebook, err := h.notebookService.CreateNotebook(c.Request.Context(), req, userID, spaceContext)/g' internal/handlers/notebook.go

echo "Done!"