# GovTech Christmas Redemption System

A Go-based redemption system with PostgreSQL persistence for managing Christmas gift redemptions.

## User Stories

### Counter Staff Functions
1. As counter staff, I want to look up a representative by staff pass ID to see which team they belong to.  
2. As counter staff, I want invalid staff pass IDs to be rejected so I do not give gifts to ineligible people.  
3. As counter staff, I want the system to use the latest mapping record (`created_at`) for a staff pass ID.  
4. As counter staff, I want to check whether a team has already redeemed its gift.  
5. As counter staff, I want an immediate "eligible/not eligible" response when I input a staff pass ID.  
6. As counter staff, I want confirmation when a redemption has been successfully recorded.  

### System Functions
7. As the system, I want to look up past redemptions by team name to determine eligibility.  
8. As the system, I want to record a new redemption with team name and `redeemed_at` timestamp when valid.  
9. As the system, I must not create a new redemption record if the team has already redeemed.  

### Representative Experience
10. As a representative, I want to be clearly informed if my team has already redeemed its gift.  

### Developer & Operations Requirements
11. As a developer, I want the redemption data store to be pluggable so storage can change without rewriting business logic.  
12. As a developer, I want unit tests for lookup, eligibility checks, and redemption creation.  
13. As an operator, I want simple commands or an HTTP API to trigger staff ID lookup and redemption.  
14. As an operator, I want clear error messages when the mapping CSV cannot be read or is malformed.
15. As an operator, I want fast look-up and low load times even when multiple redemption desks are requesting the look-up service

### Non-Functional Requirements
- Fast look-up even when under heavy load (i.e. multiple operators)
- No more than 5 minutes of redemption data dropped in the event of a crash

## Quick Start

### Prerequisites
- Docker & Docker Compose
- Go 1.21+ (for local development)

### Running with Docker

1. **Start the services:**
```bash
docker-compose up -d
```

2. **Verify the application is running:**
```bash
curl http://localhost:8080/health
```

3. **Access the API:**
- Health Check: `GET http://localhost:8080/health`
- List Redemptions: `GET http://localhost:8080/api/v1/redemptions`
- Get Redemption: `GET http://localhost:8080/api/v1/redemptions/{id}`
- Create Redemption: `POST http://localhost:8080/api/v1/redemptions`
- Update Redemption: `PUT http://localhost:8080/api/v1/redemptions/{id}`
- Delete Redemption: `DELETE http://localhost:8080/api/v1/redemptions/{id}`

### Local Development

1. **Start only the database:**
```bash
docker-compose up postgres -d
```

2. **Run the Go application:**
```bash
go run main.go
```

### Database Access

- **Host:** localhost
- **Port:** 5432
- **Database:** govtech_christmas
- **Username:** postgres
- **Password:** password123

### CSV Data

Place your CSV files in the `./data` directory. The application automatically loads them on startup.

**Staff Mappings CSV Format:** (`staff_mappings.csv`)
```csv
staff_pass_id,team_name,created_at
STAFF001,Team Alpha,1703462400000
STAFF002,Team Beta,1703548800000
STAFF003,Team Gamma,1703635200000
```

- `staff_pass_id`: Unique identifier for staff member
- `team_name`: Name of the team the staff belongs to  
- `created_at`: Timestamp when mapping was created (epoch milliseconds)

### API Endpoints

#### Health Check
```bash
GET /health
```

#### Staff Mappings
```bash
GET /api/v1/staff-mappings                    # List all staff mappings
GET /api/v1/staff-mappings/{staff_pass_id}    # Get specific staff mapping
```

#### Staff Pass Lookup & Operations
```bash
GET  /api/v1/lookup/{staff_pass_id}           # Lookup staff pass validity and team
GET  /api/v1/eligibility/{staff_pass_id}      # Check redemption eligibility
POST /api/v1/redeem/{staff_pass_id}           # Redeem gift for staff member
```

#### Redemptions Management
```bash
GET    /api/v1/redemptions       # List all redemptions
GET    /api/v1/redemptions/{id}  # Get specific redemption
POST   /api/v1/redemptions       # Create new redemption
PUT    /api/v1/redemptions/{id}  # Update redemption
DELETE /api/v1/redemptions/{id}  # Delete redemption
```

### Example API Responses

**Staff Lookup Response:**
```json
{
  "staff_pass_id": "STAFF001",
  "team_name": "Team Alpha",
  "valid": true,
  "mapping_created_at": 1703462400000
}
```

**Eligibility Check Response:**
```json
{
  "staff_pass_id": "STAFF003",
  "team_name": "Team Gamma",
  "eligible": true,
  "reason": "Team is eligible for redemption"
}
```

**Redemption Record:**
```json
{
  "id": 1,
  "team_name": "Team Alpha",
  "redeemed_at": "2025-12-15T10:30:00Z",
  "staff_pass_id": "STAFF001",
  "created_at": "2025-12-15T10:30:00Z",
  "updated_at": "2025-12-15T10:30:00Z"
}
```

**Staff Mapping Record:**
```json
{
  "id": 1,
  "staff_pass_id": "STAFF001",
  "team_name": "Team Alpha",
  "created_at": 1703462400000
}
```

## Project Structure

```
├── main.go              # Application entrypoint
├── docker-compose.yml   # Docker services configuration
├── Dockerfile           # Go application container
├── init.sql             # Database initialization
├── .env                 # Environment variables
├── data/                # CSV data directory
└── README.md            # This file
```

## Current Status

✅ Docker environment with PostgreSQL  
✅ Go application entrypoint with HTTP API  
✅ Staff pass ID to team mapping system  
✅ CSV data loading from staff_mappings.csv  
✅ Staff pass lookup functionality  
✅ Team eligibility checking  
✅ Redemption workflow with duplicate prevention  
✅ Complete CRUD API for redemptions and staff mappings  

### Core Features Working
- **Staff Pass Lookup**: Validate staff ID and get team information
- **Eligibility Checking**: Determine if team can redeem (prevents duplicates)
- **Redemption Process**: Complete workflow from lookup to redemption recording
- **Data Persistence**: All data stored in PostgreSQL with proper indexing
- **CSV Integration**: Auto-loads staff mappings from CSV file

### Next Steps (Optional Enhancements)
1. Add authentication and authorization
2. Implement audit logging
3. Add bulk operations support
4. Create admin dashboard
5. Add comprehensive error handling and validation
6. Implement rate limiting
7. Add unit and integration tests
