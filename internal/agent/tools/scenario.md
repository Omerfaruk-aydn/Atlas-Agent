Validate or exercise a version-1 web/terminal interaction scenario, or report a
persisted run. At most 64 steps and 64 explicit assertions, with a ten-minute
deadline. Validation performs no execution. Web actions retain browser tool
permissions and budgets. Terminal scenarios use real PTY input/resize/exit
observations and ordinary command permissions; required isolation never falls
back to a host terminal. Runs persist intent before effects and link bounded
artifacts to the observed source. Missing capabilities report unavailable and
never pass. Raw terminal transcript, terminal rendering and screenshots are
different artifacts. Source changes invalidate old observations. Scenario success
does not establish overall visual quality or replace independent review.
