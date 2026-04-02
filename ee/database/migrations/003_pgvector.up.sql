-- Enable pgvector extension for vector similarity search
CREATE EXTENSION IF NOT EXISTS vector;

-- Store per-RCA embeddings for dynamic pattern matching
CREATE TABLE rca_embeddings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    analysis_id UUID NOT NULL REFERENCES analyses(id) ON DELETE CASCADE,
    rca_index INT NOT NULL DEFAULT 0,
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    failure_type TEXT,
    embedding_text TEXT NOT NULL,
    embedding VECTOR(768),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (analysis_id, rca_index)
);

-- Indexes for common queries
CREATE INDEX idx_rca_embeddings_org ON rca_embeddings(org_id);
CREATE INDEX idx_rca_embeddings_vector ON rca_embeddings
    USING hnsw (embedding vector_cosine_ops);
