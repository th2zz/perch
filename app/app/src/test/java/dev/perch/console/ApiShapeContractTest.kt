package dev.perch.console

import dev.perch.console.core.herdrclient.Agent
import dev.perch.console.core.herdrclient.DecideResponse
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.core.herdrclient.DecisionsResponse
import dev.perch.console.core.herdrclient.Folder
import dev.perch.console.core.herdrclient.FoldersResponse
import dev.perch.console.core.herdrclient.ConversationMessage
import dev.perch.console.core.herdrclient.Health
import dev.perch.console.core.herdrclient.HealthItem
import dev.perch.console.core.herdrclient.HealthResponse
import dev.perch.console.core.herdrclient.Host
import dev.perch.console.core.herdrclient.KeysResponse
import dev.perch.console.core.herdrclient.Mirror
import dev.perch.console.core.herdrclient.Pane
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.core.herdrclient.UsageFacts
import dev.perch.console.core.herdrclient.Transcript
import dev.perch.console.core.herdrclient.SnapshotResponse
import dev.perch.console.core.herdrclient.StreamEvent
import dev.perch.console.core.herdrclient.Tab
import dev.perch.console.core.herdrclient.ValueResponse
import dev.perch.console.core.herdrclient.ValueStats
import dev.perch.console.core.herdrclient.Workspace
import java.io.File
import kotlinx.serialization.KSerializer
import kotlinx.serialization.descriptors.elementNames
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Every name the app reads must be a name the bridge sends.
 *
 * The two programs are in different languages, so nothing type-checks across the
 * boundary. Every model here is defaulted on purpose — a newer bridge must degrade
 * rather than crash — and that design has a sharp edge: a **renamed** field does
 * not error, it arrives as an empty string or an empty list. The project has
 * already been bitten: `pane.read` answers inside a wrapper, decoding the wrapper
 * into the inner type "succeeded", and every excerpt was blank with no error and
 * no log.
 *
 * The live tests could not catch that class, because they assert on whatever data
 * is to hand: an empty list makes a `for … { assert }` loop vacuous, and this
 * machine usually has nothing waiting. This test needs no data at all. It compares
 * the field names the app declares — read out of kotlinx serialization's own
 * descriptors, so it cannot drift from what the app actually parses — with the
 * names the bridge declares in `contracts/api-shape.json`.
 *
 * The rule is **subset**: everything the app reads must exist on the bridge. The
 * bridge may send more, and it does — the decision record carries `tab_id` and
 * `updated_at` that no screen shows.
 */
class ApiShapeContractTest {

    private data class ShapeEntry(val name: String, val fields: List<String>)

    private val json = Json { ignoreUnknownKeys = true }

    /** The logical name in the contract file, and the class the app decodes it into. */
    private fun models(): Map<String, KSerializer<*>> = mapOf(
        "mirror" to Mirror.serializer(),
        "host" to Host.serializer(),
        "workspace" to Workspace.serializer(),
        "tab" to Tab.serializer(),
        "pane" to Pane.serializer(),
        "agent" to Agent.serializer(),
        "decision" to Decision.serializer(),
        "event" to StreamEvent.serializer(),
        "session" to SessionView.serializer(),
        "value_stats" to ValueStats.serializer(),
        "decide_result" to DecideResponse.serializer(),
        "health" to Health.serializer(),
        "health_item" to HealthItem.serializer(),
        "snapshot_response" to SnapshotResponse.serializer(),
        "decisions_response" to DecisionsResponse.serializer(),
        "value_response" to ValueResponse.serializer(),
        "health_response" to HealthResponse.serializer(),
        "keys_response" to KeysResponse.serializer(),
        "usage_facts" to UsageFacts.serializer(),
        "conversation" to Transcript.serializer(),
        "conversation_message" to ConversationMessage.serializer(),
        "folder" to Folder.serializer(),
        "folders_response" to FoldersResponse.serializer(),
    )

    private fun contract(): Map<String, ShapeEntry> {
        val parsed = json.parseToJsonElement(
            ContractFixtures.text(
                "contracts/api-shape.json",
                "PERCH_UPDATE_API_FIXTURE=1 go test ./internal/server/ -run WireShape",
            ),
        )
        val types = parsed.jsonObject["types"]?.jsonArray
            ?: error("contracts/api-shape.json has no types array")
        return types.associate { element ->
            val obj = element.jsonObject
            val name = obj.getValue("name").toString().trim('"')
            val fields = obj.getValue("fields").jsonArray.map { it.toString().trim('"') }
            name to ShapeEntry(name, fields)
        }
    }

    @Test
    fun everyNameTheAppReadsIsANameTheBridgeSends() {
        val contract = contract()
        assertTrue("the shared API shape is empty", contract.isNotEmpty())

        val problems = mutableListOf<String>()
        for ((logicalName, serializer) in models()) {
            val entry = contract[logicalName]
            if (entry == null) {
                problems += "$logicalName: the bridge's contract has no entry for it"
                continue
            }
            // serializer().descriptor.elementNames is the list kotlinx itself uses
            // to find each field, so this cannot drift from what decoding does.
            val declared = serializer.descriptor.elementNames.toList()
            val missing = declared.filterNot { it in entry.fields }
            if (missing.isNotEmpty()) {
                problems += "$logicalName: the app reads $missing, which the bridge does not send " +
                    "(it sends ${entry.fields})"
            }
        }

        assertTrue(
            "the app and the bridge disagree about the wire:\n" + problems.joinToString("\n"),
            problems.isEmpty(),
        )
    }

    @Test
    fun theContractCoversEveryModelTheAppDecodes() {
        // The other direction, for the models: a model the app decodes but the
        // contract never mentions is a model nothing is checking. `error` is the
        // one deliberate omission — the bridge writes that body as a bare map in
        // about twenty handlers, and it is tracked separately.
        val contract = contract()
        val uncovered = models().keys.filterNot { it in contract }
        assertTrue("models with no contract entry: $uncovered", uncovered.isEmpty())
    }
}
