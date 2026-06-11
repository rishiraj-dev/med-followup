# Hospital Welfare Outbound Dialer

A Go-based service designed to automate outbound SIP welfare calls to patients, particularly for post-operative care and general check-ins.

## Product Overview
This application functions as an automated outbound dialer that contacts patients via the SIP protocol. It is specifically tailored for hospital or clinical welfare checks, enabling healthcare providers to automatically engage with patients after procedures (e.g., surgeries) to monitor their recovery and well-being. By utilizing AI and audio integrations, the system is designed to handle interactive conversational workflows securely and efficiently.

## Workflow
1. **Configuration**: Connects using predefined environment variables (managed via `.env`) and SIP server configurations.
2. **Patient Data Feed**: Reads target patient information (such as Patient ID, Name, Procedure, Assigned Doctor, Days Post-Op, and SIP address) from a `patients.json` file or via a CRM integration.
3. **Outbound Calling**: Initiates a SIP call to the patient's SIP endpoint using the [sipgo](https://github.com/emiago/sipgo) library.
4. **AI & Audio Interaction**: Once the call is established, the application uses its AI module (`internal/ai`) and text-to-speech/audio pipelines (`internal/audio`) to interact with the patient, asking targeted post-operative care questions and capturing responses.

## Project Structure
- `cmd/outbound_dialer/` - Main application entrypoint.
- `internal/`
  - `ai/` - AI integration for managing conversational prompts and logic.
  - `audio/` - Audio processing and stream management.
  - `config/` - Environment and application configuration handling.
  - `logger/` - Application logging.
  - `models/` - Data structures (e.g., definitions for patient data).
  - `sipdialer/` - Core SIP execution logic and routing.

## Getting Started
To run the dialer locally:

1. Ensure Go is installed on your system.
2. Create and configure your `.env` file with the necessary SIP credentials, API keys for AI handling, and system configurations.
3. Add the patients you want to dial into `patients.json`.
4. Run the application:
   ```bash
   go run cmd/outbound_dialer/main.go
   ```
