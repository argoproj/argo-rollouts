Feature: Blue-Green

  Scenario:
    Given I apply manifest "rollout-bluegreen.yaml"
    When I change the image to "green"
    And promote the rollout
    Then the active service should route traffic to new version's ReplicaSet
