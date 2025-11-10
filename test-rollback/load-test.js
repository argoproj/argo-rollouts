import http from 'k6/http';
import { sleep } from 'k6';

export const options = {
  scenarios: {
    normal_load: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '2m', target: 20 },   // Ramp up
        { duration: '5m', target: 20 },   // Steady load
        { duration: '2m', target: 50 },   // Traffic spike
        { duration: '3m', target: 50 },   // Maintain spike
        { duration: '2m', target: 20 },   // Return to normal
        { duration: '1m', target: 0 },    // Scale down
      ],
    },
  },
};

export default function () {
  const responses = http.batch([
    ['GET', 'http://example-rollout.default.svc.cluster.local/'],
    ['GET', 'http://example-rollout.default.svc.cluster.local/health'],
  ]);
  sleep(1);
}