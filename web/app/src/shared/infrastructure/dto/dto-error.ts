// The error a DTO's fromJSON throws on data that does not match the wire shape
// (ADR 0106). It names the DTO and the path of the field, so a changed wire
// shape is found in one place. An adapter turns it into its port's own error.

export class DtoError extends Error {
  readonly dto: string;
  /** The field's path inside the DTO: "projects[1].name". "" for the value itself. */
  readonly path: string;

  constructor(dto: string, path: string, problem: string) {
    super(`${dto}${path === "" ? "" : `.${path}`}: ${problem}`);
    this.name = "DtoError";
    this.dto = dto;
    this.path = path;
  }
}
