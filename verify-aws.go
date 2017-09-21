package main

import (
	"fmt"
	"time"

	"github.com/aws/amazon-ecs-cni-plugins/pkg/logger"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"

	log "github.com/cihub/seelog"
	"math/rand"
)

var testEC2Instance *awsutils.EC2InstanceMetadataCache

const (
	defaultLogFilePath = "/var/log/verify-aws.log"
)

func main() {
	var testOK = true
	defer log.Flush()
	logger.SetupLogger(logger.GetLogFileLocation(defaultLogFilePath))
	// verify API: GetInstanceMetadata
	testEC2Instance, _ = awsutils.New()

	testENIs, _ := testEC2Instance.GetAttachedENIs()
	log.Infof("Number of current attached interface is %d \n", len(testENIs))

	// detach and delete all interfaces except primary interface
	for _, eni := range testENIs {

		log.Infof("*eni %s, primary eni %s\n", eni.ENIID, testEC2Instance.GetPrimaryENI())
		if eni.ENIID == testEC2Instance.GetPrimaryENI() {
			log.Infof("skip deleting primary ENI %s\n", eni.ENIID)
			continue
		}
		testEC2Instance.FreeENI(eni.ENIID)
	}

	// verify only primary interface left
	testENIs, err := testEC2Instance.GetAttachedENIs()

	if err != nil {
		fmt.Print(" err: GetAttachedENIs()", err.Error())
		testOK = false
	}

	log.Infof("Number of current attached interface is %d \n", len(testENIs))
	if len(testENIs) != 1 {
		fmt.Println("=== Failed: GetInstanceMetadata,  expected only primary interface")
		testOK = false
	} else {
		fmt.Println("=== Passed OK: GetInstanceMetadata")
	}

	// verify allocate IP address
	testEC2Instance.AllocAllIPAddress(testEC2Instance.GetPrimaryENI())

	//verify allocate MAX of ENI
	if verifyFreeENIAWSAllocENI() != true {
		testOK = false
		fmt.Println("==== Failed: verifyFreeENIAWSAllocENI")
	} else {
		fmt.Println("===== Passed OK: verifyFreeENIAWSAllocENI")
	}

	log.Infof("DONE: verify-aws testOK %v", testOK)

}

func verifyFreeENIAWSAllocENI() bool {
	var enis []string
	var result = true

	//verify allocate MAX number of ENI
	for {
		result, eni := verifyAllocENI(false)

		if !result {
			break
		}
		fmt.Println("eni", eni)
		enis = append(enis, eni)
	}

	if len(enis) > 0 {
		//pick radom eni to delete
		eniIdx := rand.Intn(len(enis))

		log.Debugf("Verify free eniIdx %d, eni : %s", eniIdx, enis[eniIdx])
		testEC2Instance.FreeENI(enis[eniIdx])

		result, _ = verifyAllocENI(true)
	}

	return result
}

func verifyAllocENI(printResult bool) (bool, string) {

	// verify API: AllocENI
	eni, err := testEC2Instance.AllocENI()

	if err != nil {
		log.Errorf("verifyAllocENI: error from AllocENI %s", err)
	}

	// will retry maximum of 5 * 5 seconds and see if the newly created ENI showing up on the instance
	result := false
	if eni != "" && err == nil {
		err = testEC2Instance.AllocAllIPAddress(eni)
		if err != nil {
			log.Error("error on AllocAllIPAddress", err)
		}
		retry := 0
		for {
			retry++
			if retry > 5 {
				log.Info("exceed retry limit on GetInstanceMetaData")
				break
			}
			testENIs, _ := testEC2Instance.GetAttachedENIs()

			// verify eni is in the returned eni list

			for _, returnedENI := range testENIs {
				if eni == returnedENI.ENIID {
					addrs, _, _ := testEC2Instance.DescribeENI(eni)
					log.Infof("Number of address %d on eni %s", len(addrs), eni)
					result = true
					break
				}
			}

			if result != true {
				time.Sleep(5 * time.Second)
				continue
			}
			break
		}
	}

	if printResult {
		if result == true {
			log.Info("=== Passed OK: AllocENI")
		} else {
			log.Info("=== Failed: AllocENI %v", err)
		}
	}
	return result, eni
}
