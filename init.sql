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
    id SERIAL PRIMARY KEY,
    team_name VARCHAR(255) NOT NULL,
    redeemed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    staff_pass_id VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for better query performance
CREATE INDEX IF NOT EXISTS idx_staff_mappings_staff_pass_id ON staff_mappings(staff_pass_id);
CREATE INDEX IF NOT EXISTS idx_staff_mappings_team_name ON staff_mappings(team_name);
CREATE INDEX IF NOT EXISTS idx_staff_mappings_created_at ON staff_mappings(created_at);
CREATE INDEX IF NOT EXISTS idx_redemptions_team_name ON redemptions(team_name);
CREATE INDEX IF NOT EXISTS idx_redemptions_redeemed_at ON redemptions(redeemed_at);
CREATE INDEX IF NOT EXISTS idx_redemptions_staff_pass_id ON redemptions(staff_pass_id);

-- Insert sample staff mappings data for testing
INSERT INTO staff_mappings (staff_pass_id, team_name, created_at) VALUES
('STAFF001', 'Team Alpha', 1703462400000),
('STAFF002', 'Team Beta', 1703548800000),
('STAFF003', 'Team Gamma', 1703635200000),
('STAFF004', 'Team Alpha', 1703721600000),
('STAFF005', 'Team Delta', 1703808000000)
ON CONFLICT DO NOTHING;

-- Insert sample redemptions data for testing
INSERT INTO redemptions (team_name, staff_pass_id) VALUES
('Team Alpha', 'STAFF001'),
('Team Beta', 'STAFF002')
ON CONFLICT DO NOTHING;

-- Create a function to update the updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create trigger to automatically update updated_at
CREATE TRIGGER update_redemptions_updated_at 
    BEFORE UPDATE ON redemptions 
    FOR EACH ROW 
    EXECUTE FUNCTION update_updated_at_column();