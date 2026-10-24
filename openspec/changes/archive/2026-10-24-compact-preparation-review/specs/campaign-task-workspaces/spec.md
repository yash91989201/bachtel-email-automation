# Spec Delta

## ADDED Requirements

### Requirement: Preparation review uses progressive disclosure
The Recipients workspace SHALL show compact preparation and backend-review summaries before detailed evidence. It SHALL prioritize held entries, collapse routine accepted-recipient and correction lists by default, and constrain long lists to labeled scrollable regions. Counts, blockers and next actions SHALL remain outside those regions.

#### Scenario: Large prepared dataset is reviewed
- **WHEN** a completed preparation run has a backend review with hundreds of accepted entries and corrections
- **THEN** the workspace shows source-row, recipient-entry, accepted and held totals without requiring a row list to be opened
- **AND** AI correction and unresolved-finding totals are labeled separately from backend acceptance and are not added to recipient totals
- **AND** accepted recipients and resolved corrections have separate, collapsed-by-default disclosures with total counts
- **AND** opening either disclosure reveals its existing row evidence in a height-constrained scroll area rather than expanding the page by the full list length
- **AND** review-report links and finalization choices remain outside the scroll areas

#### Scenario: Held recipients need attention
- **WHEN** the reviewed prepared data contains held entries
- **THEN** the held count and correction guidance are visible without opening a disclosure
- **AND** the backend-held entry list is open by default, appears before the routine accepted-recipient list, and retains source row, position, recipient and all recorded reasons
- **AND** the backend-held list is the canonical actionable list; an additional expanded provisional AI-held recipient table does not repeat it
- **AND** detailed unresolved AI findings remain separately accessible with their field, scope and position context

#### Scenario: No recipients are held
- **WHEN** backend review reports zero held entries
- **THEN** the workspace shows a concise no-held-entries state without an empty warning table
- **AND** accepted entries and any resolved corrections remain inspectable through their count-bearing disclosures

#### Scenario: Detailed evidence is bounded or incomplete
- **WHEN** an expanded accepted, held, finding, correction, preparation-run or final-exclusion list exceeds the review area's height
- **THEN** the list scrolls within its container while surrounding summaries and controls remain in the ordinary page flow
- **AND** short lists use their content height instead of reserving a large empty panel
- **AND** table headings remain visible during vertical scrolling and all cell text remains inspectable
- **AND** any server-side truncation is explicitly labeled with shown and total counts rather than implying that scrolling reveals omitted evidence
- **AND** the selected-run sanitization report remains discoverable for complete correction/finding evidence when that download is available

#### Scenario: Backend review is unavailable or the dataset is finalized
- **WHEN** completed preparation evidence is available but backend validation is unavailable
- **THEN** the pending/refusal explanation remains visible, AI evidence remains inspectable, and no provisional count is labeled backend accepted or held
- **AND** the preparation-held evidence remains accessible without requiring a backend review
- **AND** the existing backend-review requirement for approval remains in force
- **WHEN** a final snapshot is selected
- **THEN** its accepted/excluded outcome is authoritative, its exclusion list is height-constrained, and preparation evidence is labeled as history
- **AND** preparation failures and their retry remedy remain visible even when run history is collapsed

#### Scenario: Review disclosures work on narrow screens and by keyboard
- **WHEN** the operator uses the review at 360, 768 or 1440 CSS pixels wide or navigates with a keyboard
- **THEN** disclosures expose an accessible name, expanded state and visible focus and can be opened and closed without a pointer
- **AND** overflowing list content can be reached and scrolled by keyboard, with no focus trap
- **AND** wide tables scroll inside their labeled region without causing whole-page horizontal overflow
- **AND** opening, closing or scrolling evidence causes no AI request, dataset selection, approval, upload or sending operation

### Requirement: Dataset finalization controls have clear paths
The workspace SHALL present approving reviewed prepared data as the primary finalization path and editing a downloaded sheet as an explicit secondary path. Related reports SHALL be grouped by purpose. Disclosures SHALL preserve visible errors, existing confirmation requirements and the distinction between dataset approval, formatting approval and sending authorization.

#### Scenario: Operator can approve prepared data
- **WHEN** the selected completed run has the backend review required by the existing approval rules
- **THEN** one clearly labeled primary area contains the review confirmation and Approve prepared data as-is action
- **AND** the action requires the existing explicit confirmation and refers to the reviewed selected run
- **AND** the edit/download/upload path is collapsed by default behind a clearly labeled control
- **AND** a visible explanation states that approving data does not send a test, approve received formatting or authorize candidate sending or scheduling

#### Scenario: Operator chooses to edit the data
- **WHEN** the operator opens the secondary edit path
- **THEN** it presents the prepared sending-sheet download, edit guidance and labeled final-sheet upload in that order when each operation is available
- **AND** concise guidance explains deterministic validation and no AI execution, with the existing normalization/fallback and supported-edit details accessible nearby
- **AND** opening the path does not select, replace or approve any dataset
- **AND** when no accepted entries exist the edit remedy is readily available, while existing finalization and sending availability rules still apply

#### Scenario: Review reports have one clear location
- **WHEN** review report downloads are available
- **THEN** one labeled secondary report area groups the sanitization report and applicable exclusion report with concise descriptions of their different purposes
- **AND** the sanitization description identifies outcomes, corrections, findings, reasons and original/prepared values
- **AND** the prepared sending sheet appears in the edit path and each report link has one canonical location for the displayed dataset
- **AND** unavailable reports are omitted and all links retain existing selected-run/final-snapshot precedence and owner authorization

#### Scenario: Upload is refused or controls are unavailable
- **WHEN** a final-sheet upload is rejected
- **THEN** the edit path is open on the returned page with the error and remedy visible beside the upload operation
- **AND** the prior final snapshot is retained and no partial success is reported
- **WHEN** preparation, finalization or upload is blocked by the existing task state or review prerequisites
- **THEN** the reason and available remedy remain visible outside collapsed technical evidence
- **AND** disclosures do not enable actions prohibited by started/closed-task rules or bypass owner access, explicit confirmations, snapshot-bound tests or sending authorization
