# Hatch

Hatch manages experimental projects from creation through completion, abandonment, or promotion.

## Language

**Experimental project**:
A named experiment with a creation date, status, and location. Its identity persists after promotion, and its name is unique among all tracked projects.
_Avoid_: Sandbox, prototype

**Experiments directory**:
The home of experimental projects that have not been promoted.
_Avoid_: Hatch directory

**Promotion**:
The relocation of an experimental project outside the experiments directory while preserving its tracked identity and recording its destination. Promotion is terminal in the initial lifecycle.

**Removal**:
The end of Hatch's tracking of an unpromoted project, with any existing project files moved to trash. If its files are already missing, removal clears only its record; promoted projects cannot be removed through Hatch.

**Active**:
The initial status of an experimental project, indicating ongoing exploration.

**Completed**:
The status of an experiment whose work is finished without promotion. It can become active or abandoned again.

**Abandoned**:
The status of an experiment that has been set aside without promotion. It can become active or completed again.

**Promoted**:
The terminal status of a project relocated outside the experiments directory through promotion. It remains tracked by Hatch.
