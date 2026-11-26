package dev.perch.console.core.format

import dev.perch.console.core.herdrclient.UsageFacts

object UsageSummary {

    /** One row: what it is called, and what it says. */
    data class Row(val label: String, val value: String)

    /**
     * What the panel should show, decided here so it can be tested without a screen.
     *
     * Three outcomes, because they are three different situations for the person reading them —
     * and the first two would otherwise look identical: an empty box.
     */
    sealed interface Panel {
        data class Rows(val rows: List<Row>) : Panel
        /** The agent knows nothing yet: it has written no records for this session. */
        data object NothingYet : Panel
        /** Nobody has taught Perch this agent's format. */
        data object NotReadable : Panel
    }

    fun panel(facts: UsageFacts): Panel = when {
        rows(facts).isNotEmpty() -> Panel.Rows(rows(facts))
        facts.readable -> Panel.NothingYet
        else -> Panel.NotReadable
    }

    fun rows(facts: UsageFacts): List<Row> {
        val rows = mutableListOf<Row>()

        if (facts.model.isNotBlank()) {
            rows += Row(
                "Model",
                if (facts.effort.isBlank()) facts.model else "${facts.model} · ${facts.effort} effort",
            )
        }
        contextRow(facts)?.let { rows += it }
        spendRow(facts)?.let { rows += it }
        if (facts.access.isNotBlank()) rows += Row("Access", facts.access)
        if (facts.goal.isNotBlank()) {
            rows += Row(
                "Goal",
                if (facts.goalStatus.isBlank()) facts.goal else "${facts.goalStatus} — ${facts.goal}",
            )
        }
        return rows
    }

    private fun contextRow(facts: UsageFacts): Row? {
        if (facts.contextUsed <= 0) return null
        if (facts.contextWindow <= 0) return Row("Context", "${tokens(facts.contextUsed)} used")
        val percent = (facts.contextUsed * 100.0 / facts.contextWindow).toInt()
        return Row(
            "Context",
            "$percent% · ${tokens(facts.contextUsed)} of ${tokens(facts.contextWindow)}",
        )
    }

    private fun spendRow(facts: UsageFacts): Row? {
        if (facts.totalTokens <= 0) return null
        val billed = facts.totalTokens - facts.cachedTokens
        val cachedShare = (facts.cachedTokens * 100.0 / facts.totalTokens).toInt()
        return Row(
            "Spent",
            "${tokens(billed)} not cached · ${tokens(facts.totalTokens)} total ($cachedShare% cached)",
        )
    }

    /**
     * Counts in a shape a person reads at a glance: 7.05M, 996k, 1.54B.
     *
     * Deliberately not locale-formatted with thousands separators: these numbers are compared
     * with a status line the user sees in their terminal, and "996147" and "996,147" are easier
     * to match up than "996k" is to either — which is why the exact form is kept under a
     * thousand and the scaled form above it.
     */
    fun tokens(count: Long): String = when {
        count >= 1_000_000_000 -> "%.2fB".format(count / 1_000_000_000.0)
        count >= 1_000_000 -> "%.2fM".format(count / 1_000_000.0)
        count >= 1_000 -> "%.0fk".format(count / 1_000.0)
        else -> count.toString()
    }
}
