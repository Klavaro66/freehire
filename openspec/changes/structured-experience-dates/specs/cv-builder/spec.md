## ADDED Requirements

### Requirement: A CV document's period dates are structured, not free text
An experience or education entry's start/end period within a CV document SHALL
be a structured year/(optional month) value plus a boolean marking an ongoing
entry, matching the same representation used by the structured résumé and the
experience bank — not a free-form string. Rendering a CV to PDF SHALL format
this structured value into display text (e.g. "Mar 2021", "2018", "Mar 2021 –
Present") before it reaches the template layer, so no template needs to know
the underlying value is structured, and the rendered output is unchanged from
before this value became structured.

#### Scenario: A structured period renders identically to the prior free-text output
- **WHEN** a CV document has an experience entry with a structured start of
  {year: 2018, month: 3} and no end (current)
- **THEN** the rendered PDF shows the same "Mar 2018 – Present" text a
  free-text field carrying that exact value would have rendered

#### Scenario: A year-only period renders without a fabricated month
- **WHEN** a CV document has an entry whose period carries only a year
- **THEN** the rendered PDF shows only the year for that boundary, not a
  month the candidate never specified
