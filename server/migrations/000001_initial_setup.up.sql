-- Proto Fleet PostgreSQL Initial Setup
-- This migration creates ENUM types and the updated_at trigger function

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- =====================================================
-- ENUM Type Definitions
-- =====================================================

-- Device status enum (7 values after migrations 039 and 058)
CREATE TYPE device_status_enum AS ENUM (
    'ACTIVE',
    'INACTIVE',
    'OFFLINE',
    'MAINTENANCE',
    'ERROR',
    'UNKNOWN',
    'NEEDS_MINING_POOL'
);

-- Device pairing status enum (5 values after migration 049)
CREATE TYPE pairing_status_enum AS ENUM (
    'PENDING',
    'PAIRED',
    'UNPAIRED',
    'FAILED',
    'AUTHENTICATION_NEEDED'
);

-- Command batch log status enum
CREATE TYPE batch_status_enum AS ENUM (
    'PENDING',
    'PROCESSING',
    'FINISHED'
);

-- Queue message status enum
CREATE TYPE queue_status_enum AS ENUM (
    'PENDING',
    'PROCESSING',
    'SUCCESS',
    'FAILED'
);

-- Command on device log status enum
CREATE TYPE device_command_status_enum AS ENUM (
    'SUCCESS',
    'FAILED'
);

-- =====================================================
-- Trigger function for auto-updating updated_at columns
-- (PostgreSQL equivalent of MySQL's ON UPDATE CURRENT_TIMESTAMP)
-- =====================================================

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- =====================================================
-- Helper function for ON UPDATE CURRENT_TIMESTAMP behavior on last_seen
-- =====================================================

CREATE OR REPLACE FUNCTION update_last_seen_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.last_seen = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
