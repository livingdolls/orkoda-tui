/** @jsxImportSource @opentui/react */

const navigation = ["Projects", "Jobs", "Settings", "Diagnostics"]

export function App() {
  return (
    <box flexDirection="column" width="100%" height="100%" backgroundColor="#0B1020">
      <box
        height={3}
        paddingLeft={1}
        paddingRight={1}
        flexDirection="row"
        justifyContent="space-between"
        alignItems="center"
        backgroundColor="#11182B"
      >
        <text fg="#7DD3FC">ORKODA</text>
        <text fg="#94A3B8">AI software development orchestrator</text>
        <text fg="#4ADE80">local</text>
      </box>

      <box flexGrow={1} flexDirection="row" padding={1} gap={1}>
        <box
          width={24}
          flexDirection="column"
          borderStyle="rounded"
          borderColor="#334155"
          padding={1}
          gap={1}
          title="Navigation"
        >
          {navigation.map((item, index) => (
            <text key={item} fg={index === 0 ? "#7DD3FC" : "#94A3B8"}>
              {index === 0 ? `› ${item}` : `  ${item}`}
            </text>
          ))}
        </box>

        <box
          flexGrow={1}
          flexDirection="column"
          borderStyle="rounded"
          borderColor="#334155"
          padding={1}
          gap={1}
          title="Projects"
        >
          <text fg="#E2E8F0">No project registered yet.</text>
          <text fg="#94A3B8">
            The next milestone connects this shell to the local Go daemon and repository registry.
          </text>
        </box>
      </box>

      <box
        height={1}
        paddingLeft={1}
        paddingRight={1}
        flexDirection="row"
        justifyContent="space-between"
      >
        <text fg="#64748B">Ctrl+C quit</text>
        <text fg="#64748B">protocol v1 • daemon disconnected</text>
      </box>
    </box>
  )
}
