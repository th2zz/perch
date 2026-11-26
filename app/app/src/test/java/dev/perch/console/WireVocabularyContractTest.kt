package dev.perch.console

import dev.perch.console.core.herdrclient.DecisionKind
import dev.perch.console.core.herdrclient.Speakers
import dev.perch.console.core.herdrclient.WireValues
import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The app's half of the value vocabulary.
 *
 * [ApiShapeContractTest] pins the *names* of fields; this pins the *values* inside
 * them. They fail the same way and matter just as much: the app compares these
 * strings directly — no enum, so a state herdr invents tomorrow is still
 * representable — and a renamed value does not raise, it makes a filter return
 * nothing. `state == "pending"` deciding what the queue shows is the product's
 * whole promise sitting on one string.
 */
class WireVocabularyContractTest {

    private val json = Json { ignoreUnknownKeys = true }

    /** The logical name in the contract file, and the values the app compares against. */
    private fun vocabularies(): Map<String, List<String>> = mapOf(
        "decision_state" to WireValues.DecisionState.all,
        "host_status" to WireValues.HostStatus.all,
        "agent_status" to WireValues.AgentStatus.all,
        "needs_decision_agent_status" to WireValues.AgentStatus.all.filter { WireValues.AgentStatus.needsDecision(it) },
        "decision_kind" to DecisionKind.entries.map { it.wire },
        "message_role" to Speakers.all,
    )

    private fun contract(): Map<String, List<String>> {
        val parsed = json.parseToJsonElement(
            ContractFixtures.text(
                "contracts/wire-vocabulary.json",
                "PERCH_UPDATE_VOCABULARY_FIXTURE=1 go test ./internal/core/ -run Vocabulary",
            ),
        )
        val entries = parsed.jsonObject["vocabularies"]?.jsonArray
            ?: error("contracts/wire-vocabulary.json has no vocabularies array")
        return entries.associate { element ->
            val obj = element.jsonObject
            val name = obj.getValue("name").toString().trim('"')
            val values = obj.getValue("values").jsonArray.map { it.toString().trim('"') }
            name to values
        }
    }

    @Test
    fun theValuesTheAppComparesAgainstAreTheValuesTheBridgeSends() {
        val contract = contract()
        assertTrue("the shared vocabulary is empty", contract.isNotEmpty())

        val problems = mutableListOf<String>()
        for ((name, ours) in vocabularies()) {
            val theirs = contract[name]
            if (theirs == null) {
                problems += "$name: the bridge's contract has no entry for it"
                continue
            }
            if (theirs.sorted() != ours.sorted()) {
                problems += "$name: the app compares against $ours, the bridge sends $theirs — " +
                    "a value that is not here makes the app's filter return nothing, silently"
            }
        }
        assertTrue("the app and the bridge disagree about values:\n" + problems.joinToString("\n"), problems.isEmpty())
    }

    @Test
    fun everyVocabularyInTheContractIsOneTheAppUses() {
        // The other direction: a vocabulary in the file that no app code compares
        // against is a vocabulary nothing is checking on this side, and it should
        // either gain an assertion here or leave the contract.
        val uncovered = contract().keys - vocabularies().keys
        assertEquals(
            "vocabularies in the contract that the app does not check: $uncovered",
            emptySet<String>(),
            uncovered,
        )
    }

    @Test
    fun onlyBlockedNeedsDecisionAcrossAllAgentStatuses() {
        for (status in WireValues.AgentStatus.all) {
            if (status == WireValues.AgentStatus.BLOCKED) {
                assertTrue("BLOCKED must require decision", WireValues.AgentStatus.needsDecision(status))
            } else {
                assertFalse("$status must not require decision", WireValues.AgentStatus.needsDecision(status))
            }
        }
    }

    @Test
    fun everyDeclaredAgentStatusConstantIsInAll() {
        val declared = WireValues.AgentStatus::class.java.declaredFields
            .filter { it.type == String::class.java && java.lang.reflect.Modifier.isStatic(it.modifiers) }
            .map { it.get(null) as String }
        assertEquals(
            "every declared const val in WireValues.AgentStatus must be listed in all",
            declared.toSet(),
            WireValues.AgentStatus.all.toSet(),
        )
    }
}
