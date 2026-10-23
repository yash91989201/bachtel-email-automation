# Spec Delta

## ADDED Requirements

### Requirement: Sent-copy convention for every provider
Every supported sending provider SHALL arrange Sent-folder saving for candidate and test emails, either through its native sending operation or a separate post-submission save. Its existing setup and sending configuration SHALL incorporate this requirement. Adding a provider SHALL require documenting and verifying its Sent-copy behavior; silently omitting it SHALL NOT be a supported mode.

#### Scenario: Native provider-managed saving
- **WHEN** a provider saves Sent copies as part of its sending service
- **THEN** the app keeps that native behavior enabled and does not create a second copy
- **AND** provider acceptance does not claim that asynchronous copy saving or recipient delivery has completed

#### Scenario: Application-managed saving
- **WHEN** a provider requires a separate Sent-copy operation
- **THEN** the shared dispatch flow invokes that provider's save operation only after definite acceptance is durably recorded
- **AND** it uses the same copy-failure and interruption rules as other application-managed providers

#### Scenario: New provider is introduced
- **WHEN** an adapter for another sending provider is added
- **THEN** its setup and sending configuration must satisfy the same Sent-copy convention and verify whether saving is native or separate
- **AND** existing authorization, ownership and duplicate-prevention rules continue to apply

### Requirement: Microsoft 365 native Sent Items saving
The system SHALL keep native Sent Items saving enabled for Microsoft 365 candidate and test submissions. It SHALL rely on the sending service's own copy operation, require no additional mailbox-reading permission, and SHALL NOT create an additional copy or resend because a Sent item is not immediately visible. Acceptance SHALL continue to mean accepted for processing, not confirmed delivery or completed copy saving.

#### Scenario: Microsoft candidate or test submission
- **WHEN** an authorized Microsoft 365 candidate or test send is submitted
- **THEN** its request retains native Sent Items saving for the authenticated sending mailbox
- **AND** the provider's post-submission handling performs no additional email send or manual copy operation

#### Scenario: Microsoft processing is asynchronous
- **WHEN** Microsoft accepts the request before its Sent item is visible
- **THEN** the attempt remains SUBMITTED without polling the mailbox, adding permissions or issuing a duplicate request
- **AND** the app does not claim to have confirmed the item's presence or delivery

### Requirement: Automatic Titan Sent copies
The system SHALL automatically attempt to save one copy of each future, definitely accepted Titan candidate or test email in the sending mailbox's existing Sent folder after durably recording its submission. The copy SHALL preserve the submitted message content and headers, appear as read and use the acceptance time as its mailbox timestamp. A Sent copy SHALL NOT be presented as proof of recipient delivery.

#### Scenario: Accepted candidate message
- **WHEN** Titan definitely accepts a candidate email and its submission result is saved
- **THEN** the app automatically attempts to save the submitted message in that mailbox's Sent folder
- **AND** after a successful save the operator can inspect the recipient, subject and personalized HTML in Titan webmail without an additional action
- **AND** in the uninterrupted completion path exactly one copy-saving attempt is made for that accepted submission

#### Scenario: Accepted test message
- **WHEN** Titan definitely accepts an explicitly requested test and its submission result is saved
- **THEN** its Sent copy contains the actual test recipient and sample content submitted
- **AND** saving the copy does not approve formatting, change candidate state or resume a task

#### Scenario: Rejected or uncertain submission
- **WHEN** a Titan submission is rejected before acceptance or its final acceptance is uncertain
- **THEN** the app does not create a Sent copy for that attempt
- **AND** the existing failure, safe-retry or review-required behavior applies

### Requirement: Sent copies stay within the sending connection
The system SHALL save a Sent copy only in the mailbox bound to its submission, using that connection's confidential authorization. Separate copy-saving operations SHALL use certificate-validated encryption and SHALL NOT fall back to another operator's mailbox or a replacement connection. Connection validation SHALL NOT create Sent copies.

#### Scenario: Independent Titan operators
- **WHEN** two operators send through separate Titan connections
- **THEN** each copy is saved only in the mailbox that submitted its message
- **AND** neither operator's credentials or message content are used for the other mailbox

#### Scenario: Connection changes during copy saving
- **WHEN** disconnect or replacement is requested while an accepted message's copy operation is active
- **THEN** that operation settles or ends within its deadline before the old connection is removed
- **AND** the copy is never redirected to the replacement mailbox

#### Scenario: Validate connection without sending
- **WHEN** an operator validates or reconnects a Titan mailbox without submitting an email
- **THEN** no email or Sent copy is created

#### Scenario: Microsoft submission
- **WHEN** a message is sent through Microsoft 365
- **THEN** the common provider contract retains Microsoft native Sent Items saving without a Titan copy-saving operation

### Requirement: Forward-only Sent-folder saving
The system SHALL apply the provider Sent-copy contract only to submissions performed by the updated sending path. It SHALL leave earlier attempts and the first run untouched and SHALL NOT introduce a historical backfill, copy-retry control, settings toggle or additional operator action.

#### Scenario: Deploy after the first sending run
- **WHEN** the updated app starts with existing submission history
- **THEN** it does not append copies of earlier messages, resend them or change their stored outcomes
- **AND** future accepted sends use automatic Sent-folder saving without a new button or setup step

#### Scenario: Existing authorization still applies
- **WHEN** an operator has not approved received formatting for the exact configuration or explicitly authorized candidate sending
- **THEN** automatic Sent-folder saving does not authorize a test, candidate start, retry or resend
