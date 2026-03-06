Initial commit brainstorming and design:

Main functions
1. As counter staff, I want to look up a representative by staff pass ID to see which team they belong to.  
2. As counter staff, I want invalid staff pass IDs to be rejected so I do not give gifts to ineligible people.  
3. As counter staff, I want the system to use the latest mapping record (`created_at`) for a staff pass ID.  
5. As counter staff, I want to check whether a team has already redeemed its gift.  
6. As the system, I want to look up past redemptions by team name to determine eligibility.  
7. As a representative, I want to be clearly informed if my team has already redeemed its gift.  
8. As counter staff, I want an immediate “eligible/not eligible” response when I input a staff pass ID.  
9. As the system, I want to record a new redemption with team name and `redeemed_at` timestamp when valid.  
10. As the system, I must not create a new redemption record if the team has already redeemed.  
11. As counter staff, I want confirmation when a redemption has been successfully recorded.  

Non-functionality stories
12. As a developer, I want the redemption data store to be pluggable so storage can change without rewriting business logic.  
13. As a developer, I want unit tests for lookup, eligibility checks, and redemption creation.  
14. As an operator, I want simple commands or an HTTP API to trigger staff ID lookup and redemption.  
15. As an operator, I want clear error messages when the mapping CSV cannot be read or is malformed.
16. As an operator, I want fast look-up and low load times even when multipled redemption desks are requesting the look-up service


Non-functionality requirements
1. Fast look-up even when under heavy load (i.e. multiple operators)
2. No more than 5 minutes of redemption data dropped in the event of a crash


Current to-do:
1. Implement basic redemption logic
2. Create docker environment
3. Start on PostgreSQL schema