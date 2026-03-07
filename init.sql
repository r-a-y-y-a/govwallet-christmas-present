-- Initial database setup for GovTech Christmas Redemption System

-- Create the staff pass mappings table
CREATE TABLE IF NOT EXISTS staff_mappings (
    id SERIAL PRIMARY KEY,
    staff_pass_id VARCHAR(255) NOT NULL,
    team_name VARCHAR(255) NOT NULL,
    created_at BIGINT NOT NULL, -- epoch milliseconds
    UNIQUE(staff_pass_id, created_at)
);

-- Create the main redemptions table
CREATE TABLE IF NOT EXISTS redemptions (
    team_name VARCHAR(255) UNIQUE NOT NULL,
    redeemed BOOLEAN NOT NULL DEFAULT FALSE,
    redeemed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for better query performance
CREATE INDEX IF NOT EXISTS idx_staff_mappings_staff_pass_id ON staff_mappings(staff_pass_id);
CREATE INDEX IF NOT EXISTS idx_staff_mappings_team_name ON staff_mappings(team_name);
CREATE INDEX IF NOT EXISTS idx_staff_mappings_created_at ON staff_mappings(created_at);
CREATE INDEX IF NOT EXISTS idx_redemptions_team_name ON redemptions(team_name);
CREATE INDEX IF NOT EXISTS idx_redemptions_redeemed_at ON redemptions(redeemed_at);

-- Insert sample staff mappings data for testing
INSERT INTO staff_mappings (staff_pass_id, team_name, created_at) VALUES
('STAFF001', 'Team Alpha', 1703462400000),
('STAFF002', 'Team Beta', 1703548800000),
('STAFF003', 'Team Gamma', 1703635200000),
('STAFF004', 'Team Alpha', 1703721600000),
('STAFF005', 'Team Delta', 1703808000000)
ON CONFLICT DO NOTHING;

-- Insert sample redemptions data for testing
INSERT INTO redemptions (team_name, redeemed) VALUES
('Team Alpha', TRUE),
('Team Beta', TRUE)
ON CONFLICT DO NOTHING;
