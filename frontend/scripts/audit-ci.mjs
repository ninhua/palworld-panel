import { spawnSync } from 'node:child_process';

const allowedAdvisory = 'GHSA-qwww-vcr4-c8h2';
const allowedPackage = 'react-router';
const allowedDependent = 'react-router-dom';
const failingSeverities = new Set(['high', 'critical']);

const npmExecPath = process.env.npm_execpath;
const npmCommand = npmExecPath ? process.execPath : process.platform === 'win32' ? 'npm.cmd' : 'npm';
const npmArguments = npmExecPath ? [npmExecPath, 'audit', '--json'] : ['audit', '--json'];
const result = spawnSync(npmCommand, npmArguments, {
  encoding: 'utf8',
});

if (result.error || !result.stdout.trim()) {
  console.error(result.error?.message || result.stderr || 'npm audit produced no JSON output');
  process.exit(1);
}

let report;
try {
  report = JSON.parse(result.stdout);
} catch (error) {
  console.error(`unable to parse npm audit output: ${error.message}`);
  process.exit(1);
}

if (report.error || ![0, 1].includes(result.status)) {
  console.error(report.error?.summary || result.stderr || `npm audit failed with exit code ${result.status}`);
  process.exit(1);
}

const vulnerabilities = Object.entries(report.vulnerabilities || {});
if (result.status === 1 && vulnerabilities.length === 0) {
  console.error('npm audit reported a failure without vulnerability details');
  process.exit(1);
}
const routerFinding = vulnerabilities.find(([name]) => name === allowedPackage)?.[1];
const routerAdvisories = routerFinding?.via?.filter((entry) => typeof entry === 'object') || [];
const routerFindingIsAllowed =
  failingSeverities.has(routerFinding?.severity) &&
  routerAdvisories.length > 0 &&
  routerAdvisories.every((entry) => entry.url?.endsWith(allowedAdvisory));

const remaining = vulnerabilities.filter(([name, finding]) => {
  if (!failingSeverities.has(finding.severity)) return false;
  if (name === allowedPackage && routerFindingIsAllowed) return false;
  if (
    name === allowedDependent &&
    routerFindingIsAllowed &&
    finding.via?.length > 0 &&
    finding.via.every((entry) => entry === allowedPackage)
  ) {
    return false;
  }
  return true;
});

if (remaining.length > 0) {
  for (const [name, finding] of remaining) {
    console.error(`${name}: ${finding.severity} (${finding.range})`);
    for (const entry of finding.via || []) {
      if (typeof entry === 'object') console.error(`  ${entry.title}: ${entry.url}`);
    }
  }
  process.exit(1);
}

if (routerFindingIsAllowed) {
  console.log(
    `npm audit: allowing ${allowedAdvisory}; PalPanel uses BrowserRouter as a client-only SPA and does not enable React Router RSC mode`,
  );
} else {
  console.log('npm audit: no high or critical vulnerabilities found');
}
