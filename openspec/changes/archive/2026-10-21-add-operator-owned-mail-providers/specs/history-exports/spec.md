## MODIFIED Requirements

### Requirement: Historical email downloads
The system SHALL provide an HTML download for each recorded personalized email attempt, displaying its stored recipient, subject, body and outcome metadata without recomputing from current campaign or candidate data. It SHALL NOT substitute the currently connected sender as historical evidence; retaining previous sender identities is not required.

#### Scenario: Template changed after sending
- **WHEN** the operator downloads an earlier attempt after the campaign template changes
- **THEN** the HTML contains the earlier attempt's stored subject/body
- **AND** it labels uncertain or manually resolved outcomes accurately

### Requirement: Private and inert downloads
The system SHALL authorize every original-file and generated-file download against its owning operator, exclude tokens/secrets, escape generated report text and provide static HTML that cannot execute candidate-supplied scripts.

#### Scenario: Spreadsheet formula-like text
- **WHEN** a candidate name or error beginning with a formula prefix is written into a generated report
- **THEN** it is stored as text rather than promoted to an executable formula

#### Scenario: Direct private file URL
- **WHEN** an unauthenticated visitor accesses a stored download URL
- **THEN** no file content is returned

#### Scenario: Another operator guesses an artifact URL
- **WHEN** an authenticated operator requests another operator's upload, duplicate report, task result, campaign report, ZIP or attempt HTML
- **THEN** no content or private metadata is returned
- **AND** the same ownership boundary applies to generation, regeneration and deletion actions

#### Scenario: Background export ownership
- **WHEN** an export is generated or recovered without an open browser
- **THEN** its input snapshot and output belong only to the recorded owner
- **AND** direct file storage access cannot bypass the owner check


#### Scenario: Provider application credentials excluded
- **WHEN** task reports, campaign reports, historical HTML or ZIP packages are generated after provider setup or application-secret rotation
- **THEN** no client secret, mailbox password, access token or refresh token is included
- **AND** same-mailbox settings changes do not erase or relabel existing submission outcomes

### Requirement: Manual retention and deletion
The system SHALL retain uploads, revisions, candidate/task history, attempts and generated artifacts without automatic expiry, except for the explicitly confirmed mailbox-replacement reset. Manual deletion SHALL clearly distinguish generated-file removal from deleting campaign records and require confirmation of the chosen scope.

#### Scenario: Remove generated workbook
- **WHEN** the operator deletes only a generated workbook
- **THEN** candidate eligibility and attempt history remain unchanged
- **AND** the workbook can be regenerated from retained records

#### Scenario: Delete campaign history
- **WHEN** the operator confirms deletion of an inactive campaign and all associated data
- **THEN** its live records and files are removed
- **AND** the UI has explained that its duplicate-prevention history is also removed and future campaigns can send again

#### Scenario: Delete active data
- **WHEN** deletion would remove data referenced by queued or in-flight work
- **THEN** deletion is rejected until the work is stopped and active requests settle

#### Scenario: Replace mailbox and retain templates
- **WHEN** an operator confirms mailbox replacement
- **THEN** their tasks, candidate data, original candidate workbooks, duplicate reports, approvals, attempts, resolutions and generated results/history are deleted
- **AND** their campaigns, configuration revisions and original template DOCX files are retained
- **AND** other operators' records and artifacts are unchanged

#### Scenario: Late exporter after reset
- **WHEN** a generation worker completes work for a task deleted by mailbox replacement
- **THEN** the obsolete artifact is not published or made downloadable
- **AND** reset recovery cleans remaining deleted-scope files without deleting retained templates

